package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type BlogPostRequest struct {
	Post model.BlogPost `json:"post"`
}

func GetPublishedBlogPosts(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	posts, total, err := model.GetPublishedBlogPosts(c.Query("locale"), pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(posts)
	common.ApiSuccess(c, pageInfo)
}

func GetPublishedBlogPost(c *gin.Context) {
	post, err := model.GetPublishedBlogPost(c.Query("locale"), c.Param("slug"))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "Article not found"})
			return
		}
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, post)
}

func AdminListBlogPosts(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	posts, total, err := model.AdminListBlogPosts(model.AdminBlogPostQuery{
		Locale:  c.Query("locale"),
		Status:  c.Query("status"),
		Keyword: c.Query("keyword"),
		Offset:  pageInfo.GetStartIdx(),
		Limit:   pageInfo.GetPageSize(),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(posts)
	common.ApiSuccess(c, pageInfo)
}

func AdminGetBlogPost(c *gin.Context) {
	id, err := parseBlogPostId(c)
	if err != nil {
		common.ApiErrorMsg(c, "Invalid article id")
		return
	}
	post, err := model.GetBlogPostById(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "Article not found"})
			return
		}
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, post)
}

func AdminCreateBlogPost(c *gin.Context) {
	var req BlogPostRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "Invalid article payload")
		return
	}
	post := req.Post
	post.Id = 0
	if err := model.ValidateAndNormalizeBlogPost(&post); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	if post.Status == model.BlogPostStatusPublished && post.PublishedAt == 0 {
		post.PublishedAt = common.GetTimestamp()
	}
	if id := c.GetInt("id"); id > 0 {
		post.AuthorId = id
	}
	if err := model.CreateBlogPost(&post); err != nil {
		if model.IsBlogPostSlugConflict(err) {
			common.ApiErrorMsg(c, "Article slug already exists for this locale")
			return
		}
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, post)
}

func AdminUpdateBlogPost(c *gin.Context) {
	id, err := parseBlogPostId(c)
	if err != nil {
		common.ApiErrorMsg(c, "Invalid article id")
		return
	}
	var req BlogPostRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "Invalid article payload")
		return
	}
	post := req.Post
	post.Id = id
	if err := model.ValidateAndNormalizeBlogPost(&post); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	if post.Status == model.BlogPostStatusPublished && post.PublishedAt == 0 {
		post.PublishedAt = common.GetTimestamp()
	}
	if err := model.UpdateBlogPost(&post); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "Article not found"})
			return
		}
		if model.IsBlogPostSlugConflict(err) {
			common.ApiErrorMsg(c, "Article slug already exists for this locale")
			return
		}
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func AdminDeleteBlogPost(c *gin.Context) {
	id, err := parseBlogPostId(c)
	if err != nil {
		common.ApiErrorMsg(c, "Invalid article id")
		return
	}
	if err := model.DeleteBlogPost(id); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func AdminPublishBlogPost(c *gin.Context) {
	updateBlogPostPublishState(c, true)
}

func AdminUnpublishBlogPost(c *gin.Context) {
	updateBlogPostPublishState(c, false)
}

func updateBlogPostPublishState(c *gin.Context, published bool) {
	id, err := parseBlogPostId(c)
	if err != nil {
		common.ApiErrorMsg(c, "Invalid article id")
		return
	}
	post, err := model.SetBlogPostPublished(id, published)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "Article not found"})
			return
		}
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, post)
}

func parseBlogPostId(c *gin.Context) (int, error) {
	id, err := strconv.Atoi(strings.TrimSpace(c.Param("id")))
	if err != nil || id <= 0 {
		return 0, errors.New("invalid id")
	}
	return id, nil
}
