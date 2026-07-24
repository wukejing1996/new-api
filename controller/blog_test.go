package controller

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type publishedBlogListResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Items []model.BlogPostListItem `json:"items"`
	} `json:"data"`
}

func TestPublishedBlogListUsesSeparateCoverEndpoint(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.BlogPost{}))

	imageData := []byte("image-bytes")
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(imageData)
	post := model.BlogPost{
		Slug:        "cached-cover",
		Title:       "Cached cover",
		ContentHTML: "<p>Body</p>",
		CoverImage:  dataURL,
		Status:      model.BlogPostStatusPublished,
		PublishedAt: 1,
	}
	require.NoError(t, db.Create(&post).Error)

	listRecorder := httptest.NewRecorder()
	listContext, _ := gin.CreateTestContext(listRecorder)
	listContext.Request = httptest.NewRequest(http.MethodGet, "/api/blog/posts?p=1&page_size=10", nil)
	GetPublishedBlogPosts(listContext)

	require.Equal(t, http.StatusOK, listRecorder.Code)
	require.NotContains(t, listRecorder.Body.String(), dataURL)
	var listResponse publishedBlogListResponse
	require.NoError(t, common.Unmarshal(listRecorder.Body.Bytes(), &listResponse))
	require.True(t, listResponse.Success)
	require.Len(t, listResponse.Data.Items, 1)
	require.True(t, listResponse.Data.Items[0].HasCoverImage)
	require.Empty(t, listResponse.Data.Items[0].CoverImage)

	coverRecorder := httptest.NewRecorder()
	coverContext, _ := gin.CreateTestContext(coverRecorder)
	coverContext.Params = gin.Params{{Key: "id", Value: strconv.Itoa(post.Id)}}
	coverContext.Request = httptest.NewRequest(http.MethodGet, "/api/blog/covers/1", nil)
	GetPublishedBlogPostCover(coverContext)

	require.Equal(t, http.StatusOK, coverRecorder.Code)
	require.Equal(t, "image/png", coverRecorder.Header().Get("Content-Type"))
	require.Equal(t, "public, max-age=31536000, immutable", coverRecorder.Header().Get("Cache-Control"))
	require.Equal(t, imageData, coverRecorder.Body.Bytes())
}

func TestBlogViewCountIsInitializedOnlyOnFirstPublish(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.BlogPost{}))

	draft := model.BlogPost{Slug: "draft", Title: "Draft", ContentHTML: "<p>Body</p>", Status: model.BlogPostStatusDraft}
	require.NoError(t, model.CreateBlogPost(&draft))
	require.Zero(t, draft.ViewCount)

	draft.Status = model.BlogPostStatusPublished
	draft.PublishedAt = 1
	require.NoError(t, model.UpdateBlogPost(&draft))
	require.NoError(t, db.First(&draft, draft.Id).Error)
	require.GreaterOrEqual(t, draft.ViewCount, int64(250))
	require.LessOrEqual(t, draft.ViewCount, int64(400))

	initialViewCount := draft.ViewCount
	draft.Title = "Updated"
	require.NoError(t, model.UpdateBlogPost(&draft))
	require.NoError(t, db.First(&draft, draft.Id).Error)
	require.Equal(t, initialViewCount, draft.ViewCount)

	draft.Status = model.BlogPostStatusDraft
	draft.PublishedAt = 0
	require.NoError(t, model.UpdateBlogPost(&draft))
	draft.Status = model.BlogPostStatusPublished
	require.NoError(t, model.UpdateBlogPost(&draft))
	require.NoError(t, db.First(&draft, draft.Id).Error)
	require.Equal(t, initialViewCount, draft.ViewCount)
}
