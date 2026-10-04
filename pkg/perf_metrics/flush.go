package perfmetrics

import (
	"fmt"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
)

func flushLoop() {
	for {
		interval := perf_metrics_setting.GetFlushIntervalMinutes()
		common.SysLog(fmt.Sprintf("PERF_DIAG stage=flush_wait interval_minutes=%d", interval))
		time.Sleep(time.Duration(interval) * time.Minute)
		setting := perf_metrics_setting.GetSetting()
		common.SysLog(fmt.Sprintf("PERF_DIAG stage=flush_tick enabled=%t bucket_time=%q retention_days=%d", setting.Enabled, setting.BucketTime, setting.RetentionDays))
		if !setting.Enabled {
			common.SysLog("PERF_DIAG stage=flush_skip reason=disabled")
			continue
		}
		flushCompletedBuckets()
		cleanupExpiredMetrics(setting.RetentionDays)
	}
}

func flushCompletedBuckets() {
	currentBucket := bucketStart(time.Now().Unix())
	visited, pending, empty, written, failed := 0, 0, 0, 0, 0
	common.SysLog(fmt.Sprintf("PERF_DIAG stage=flush_begin current_bucket_ts=%d", currentBucket))
	hotBuckets.Range(func(key, value any) bool {
		visited++
		k := key.(bucketKey)
		if k.bucketTs >= currentBucket {
			pending++
			return true
		}

		bucket := value.(*atomicBucket)
		drained := bucket.drain()
		if drained.requestCount == 0 {
			empty++
			deleteOldEmptyBucket(k, key)
			return true
		}

		common.SysLog(fmt.Sprintf("PERF_DIAG stage=db_write_begin model=%q group=%q bucket_ts=%d request_count=%d success_count=%d", k.model, k.group, k.bucketTs, drained.requestCount, drained.successCount))
		err := model.UpsertPerfMetric(&model.PerfMetric{
			ModelName:      k.model,
			Group:          k.group,
			BucketTs:       k.bucketTs,
			RequestCount:   drained.requestCount,
			SuccessCount:   drained.successCount,
			TotalLatencyMs: drained.totalLatencyMs,
			TtftSumMs:      drained.ttftSumMs,
			TtftCount:      drained.ttftCount,
			OutputTokens:   drained.outputTokens,
			GenerationMs:   drained.generationMs,
		})
		if err != nil {
			failed++
			bucket.addCounters(drained)
			common.SysLog(fmt.Sprintf("PERF_DIAG stage=db_write_failed model=%q group=%q bucket_ts=%d restored_to_memory=true", k.model, k.group, k.bucketTs))
			common.SysError(fmt.Sprintf("failed to flush perf metric bucket model=%s group=%s bucket=%d: %s", k.model, k.group, k.bucketTs, err.Error()))
			return true
		}
		written++
		common.SysLog(fmt.Sprintf("PERF_DIAG stage=db_written model=%q group=%q bucket_ts=%d request_count=%d success_count=%d", k.model, k.group, k.bucketTs, drained.requestCount, drained.successCount))

		deleteOldEmptyBucket(k, key)
		return true
	})
	common.SysLog(fmt.Sprintf("PERF_DIAG stage=flush_done visited_buckets=%d current_buckets=%d empty_buckets=%d written_buckets=%d failed_buckets=%d", visited, pending, empty, written, failed))
}

func deleteOldEmptyBucket(k bucketKey, rawKey any) {
	if k.bucketTs < bucketStart(time.Now().Add(-24*time.Hour).Unix()) {
		hotBuckets.Delete(rawKey)
	}
}

func cleanupExpiredMetrics(retentionDays int) {
	if retentionDays <= 0 {
		return
	}
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour).Unix()
	if err := model.DeletePerfMetricsBefore(cutoff); err != nil {
		common.SysError("failed to cleanup expired perf metrics: " + err.Error())
	}
}

func redisCounters(values map[string]string) counters {
	return counters{
		requestCount:   parseRedisInt(values["req"]),
		successCount:   parseRedisInt(values["ok"]),
		totalLatencyMs: parseRedisInt(values["lat"]),
		ttftSumMs:      parseRedisInt(values["ttft"]),
		ttftCount:      parseRedisInt(values["ttft_n"]),
		outputTokens:   parseRedisInt(values["out"]),
		generationMs:   parseRedisInt(values["gen_ms"]),
	}
}

func parseRedisInt(value string) int64 {
	if value == "" {
		return 0
	}
	parsed, _ := strconv.ParseInt(value, 10, 64)
	return parsed
}
