package controller

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
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
		OGImage:     dataURL,
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

func TestAdminBlogListSeparatesImagesAndPreservesStatusFilters(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.BlogPost{}))
	imageData := []byte("private-cover-bytes")
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(imageData)
	for _, status := range []string{model.BlogPostStatusDraft, model.BlogPostStatusPublished} {
		post := model.BlogPost{Slug: status, Title: status, ContentHTML: "<p>Body</p>", Status: status, CoverImage: dataURL, OGImage: dataURL}
		require.NoError(t, db.Create(&post).Error)
	}

	for _, status := range []string{"", model.BlogPostStatusDraft, model.BlogPostStatusPublished} {
		t.Run("status="+status, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/blog/admin/posts?view=list&page_size=100&status="+status, nil)
			AdminListBlogPosts(c)
			require.Equal(t, http.StatusOK, recorder.Code)
			require.NotContains(t, recorder.Body.String(), dataURL)
			require.Equal(t, "private, no-store", recorder.Header().Get("Cache-Control"))
			var response publishedBlogListResponse
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			require.True(t, response.Success)
			if status == "" {
				require.Len(t, response.Data.Items, 2)
			} else {
				require.Len(t, response.Data.Items, 1)
				require.Equal(t, status, response.Data.Items[0].Status)
			}
			for _, item := range response.Data.Items {
				require.True(t, item.HasCoverImage)
				require.Empty(t, item.CoverImage)
				require.Empty(t, item.OGImage)
			}
		})
	}

	for _, tc := range []struct {
		name    string
		handler gin.HandlerFunc
		code    int
	}{
		{"public", GetPublishedBlogPostCover, http.StatusNotFound},
		{"admin", AdminGetBlogPostCover, http.StatusOK},
	} {
		t.Run("draft-cover-"+tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Params = gin.Params{{Key: "id", Value: "1"}}
			c.Request = httptest.NewRequest(http.MethodGet, "/covers/1", nil)
			tc.handler(c)
			require.Equal(t, tc.code, recorder.Code)
			require.Equal(t, "private, no-store", recorder.Header().Get("Cache-Control"))
			if tc.code == http.StatusOK {
				require.Equal(t, imageData, recorder.Body.Bytes())
			} else {
				require.NotContains(t, recorder.Body.String(), string(imageData))
			}
		})
	}

	// The default view remains compatible with other management clients.
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/blog/admin/posts", nil)
	AdminListBlogPosts(c)
	require.Contains(t, recorder.Body.String(), dataURL)
}

func TestBlogCoverRejectsAnonymousAndInvalidIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	admin := r.Group("/api/blog/admin", middleware.AdminAuth())
	admin.GET("/covers/:id", AdminGetBlogPostCover)
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/blog/admin/covers/1", nil))
	require.Equal(t, http.StatusUnauthorized, recorder.Code)

	for _, id := range []string{"0", "-1", "invalid"} {
		for _, handler := range []gin.HandlerFunc{GetPublishedBlogPostCover, AdminGetBlogPostCover} {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Params = gin.Params{{Key: "id", Value: id}}
			c.Request = httptest.NewRequest(http.MethodGet, "/covers/"+id, nil)
			handler(c)
			require.Equal(t, http.StatusNotFound, recorder.Code)
			require.Equal(t, "private, no-store", recorder.Header().Get("Cache-Control"))
		}
	}
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
