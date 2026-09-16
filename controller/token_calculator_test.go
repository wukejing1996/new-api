package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenCalculatorHTTPContract(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	oldGroups, oldRatios, oldSpecial := setting.UserUsableGroups2JSONString(), ratio_setting.GroupRatio2JSONString(), ratio_setting.GroupGroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(oldGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(oldRatios))
		require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(oldSpecial))
		model.InvalidatePricingCache()
	})
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"calculator-public":"Public"}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"calculator-public":0.5,"calculator-private":0.1}`))
	require.NoError(t, ratio_setting.UpdateGroupGroupRatioByJSONString(`{"calculator-member":{"calculator-public":0.2}}`))
	withTieredBillingConfig(t, map[string]string{"calculator-test-model": "tiered_expr"}, map[string]string{"calculator-test-model": `tier("base", p*2+c*4)`})
	require.NoError(t, db.Create(&model.Ability{Group: "calculator-public", Model: "calculator-test-model", ChannelId: 1, Enabled: true}).Error)
	require.NoError(t, db.Create(&model.User{Id: 91234, Username: "calculator-member", Password: "test", Group: "calculator-member", Quota: 123456, Status: common.UserStatusEnabled}).Error)
	service.InitTokenEncoders()
	for _, tc := range []struct {
		name, body string
		id, status int
		cost       float64
	}{
		{"public", `{"model":"calculator-test-model","group":"calculator-public","input":{"type":"tokens","tokens":1000},"output":{"type":"tokens","tokens":100},"request_count":2}`, 0, 200, 0.0024},
		{"member", `{"model":"calculator-test-model","group":"calculator-public","input":{"type":"tokens","tokens":1000},"output":{"type":"tokens","tokens":100},"request_count":2}`, 91234, 200, 0.00096},
		{"both", `{"model":"calculator-test-model","group":"calculator-public","input":{"type":"text","text":"hi","tokens":0},"output":{"type":"tokens","tokens":0},"request_count":1}`, 0, 400, 0},
		{"private", `{"model":"calculator-test-model","group":"calculator-private","input":{"type":"tokens","tokens":0},"output":{"type":"tokens","tokens":0},"request_count":1}`, 0, 403, 0},
		{"unknown", `{"model":"not-in-catalog","group":"calculator-public","input":{"type":"tokens","tokens":0},"output":{"type":"tokens","tokens":0},"request_count":1}`, 0, 400, 0},
		{"large_body", strings.Repeat(" ", (2<<20)+1), 0, 413, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/token-calculator/estimate", strings.NewReader(tc.body))
			if tc.id != 0 {
				c.Set("id", tc.id)
			}
			EstimateTokenCalculator(c)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			if tc.status == 200 {
				var result struct {
					Success bool
					Data    service.CalculatorQuote
				}
				require.NoError(t, common.Unmarshal(w.Body.Bytes(), &result))
				assert.True(t, result.Success)
				assert.InDelta(t, tc.cost, result.Data.Discounted, 1e-12)
			}
		})
	}
	var user model.User
	require.NoError(t, db.First(&user, 91234).Error)
	assert.Equal(t, 123456, user.Quota, "estimation must not reserve or deduct quota")
	assert.Zero(t, user.UsedQuota)
}
