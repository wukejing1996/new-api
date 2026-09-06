package controller

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"golang.org/x/image/webp"
	"gorm.io/gorm"
)

const (
	ticketMaxRequestBytes int64 = 12 * 1024 * 1024
	ticketMaxImageBytes   int64 = 2 * 1024 * 1024
	ticketMaxImageCount         = 3
)

type ticketMessageResponse struct {
	Id          int                   `json:"id"`
	SenderType  string                `json:"sender_type"`
	SenderLabel string                `json:"sender_label"`
	Content     string                `json:"content"`
	Images      []ticketImageResponse `json:"images"`
	CreatedAt   int64                 `json:"created_at"`
}

type ticketImageResponse struct {
	Id       int    `json:"id"`
	FileName string `json:"file_name"`
	MimeType string `json:"mime_type"`
	FileSize int64  `json:"file_size"`
}

type ticketResponse struct {
	Id                    int                     `json:"id"`
	UserId                int                     `json:"user_id"`
	UserName              string                  `json:"user_name,omitempty"`
	Subject               string                  `json:"subject"`
	Category              string                  `json:"category"`
	Priority              string                  `json:"priority"`
	Status                string                  `json:"status"`
	LastMessageSenderType string                  `json:"last_message_sender_type"`
	CreatedAt             int64                   `json:"created_at"`
	UpdatedAt             int64                   `json:"updated_at"`
	Unread                bool                    `json:"unread,omitempty"`
	Unreplied             bool                    `json:"unreplied,omitempty"`
	Messages              []ticketMessageResponse `json:"messages,omitempty"`
}

type ticketCountResponse struct {
	UnreadCount    int64 `json:"unread_count"`
	UnrepliedCount int64 `json:"unreplied_count"`
}

type ticketStatusRequest struct {
	Status string `json:"status"`
}

func ListUserTickets(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	tickets, total, err := model.ListTickets(model.TicketListQuery{
		UserId:  c.GetInt("id"),
		Status:  c.Query("status"),
		Keyword: strings.TrimSpace(c.Query("keyword")),
		Offset:  pageInfo.GetStartIdx(),
		Limit:   pageInfo.GetPageSize(),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	items := make([]ticketResponse, 0, len(tickets))
	for _, ticket := range tickets {
		items = append(items, buildTicketResponse(&ticket, true))
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func CreateUserTicket(c *gin.Context) {
	subject, category, priority, content, images, err := parseTicketMultipart(c, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	subject, category, priority, content, err = model.ValidateTicketInput(subject, category, priority, content)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	ticket, err := model.CreateTicket(c.GetInt("id"), subject, category, priority, content, images)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	response, err := loadTicketResponse(ticket, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, response)
}

func GetUserTicket(c *gin.Context) {
	ticket, err := getTicketFromParam(c)
	if err != nil {
		common.ApiErrorMsg(c, "invalid ticket id")
		return
	}
	if ticket.UserId != c.GetInt("id") {
		common.ApiErrorMsg(c, "ticket not found")
		return
	}
	if err := model.MarkTicketUserRead(ticket.Id, c.GetInt("id")); err != nil {
		common.ApiError(c, err)
		return
	}
	response, err := loadTicketResponse(ticket, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, response)
}

func ReplyUserTicket(c *gin.Context) {
	ticket, err := getTicketFromParam(c)
	if err != nil {
		common.ApiErrorMsg(c, "invalid ticket id")
		return
	}
	userId := c.GetInt("id")
	if ticket.UserId != userId {
		common.ApiErrorMsg(c, "ticket not found")
		return
	}
	_, _, _, content, images, err := parseTicketMultipart(c, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	_, _, _, content, err = model.ValidateTicketInput(ticket.Subject, ticket.Category, ticket.Priority, content)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	updatedTicket, _, err := model.AddTicketMessage(ticket.Id, userId, model.TicketSenderUser, content, images)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	response, err := loadTicketResponse(updatedTicket, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, response)
}

func CloseUserTicket(c *gin.Context) {
	ticket, err := getTicketFromParam(c)
	if err != nil {
		common.ApiErrorMsg(c, "invalid ticket id")
		return
	}
	if err := model.CloseTicket(ticket.Id, c.GetInt("id")); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			common.ApiErrorMsg(c, "ticket not found")
			return
		}
		common.ApiError(c, err)
		return
	}
	ticket.Status = model.TicketStatusClosed
	common.ApiSuccess(c, buildTicketResponse(ticket, true))
}

func AdminListTickets(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	tickets, total, err := model.ListTickets(model.TicketListQuery{
		Status:    c.Query("status"),
		Category:  c.Query("category"),
		Priority:  c.Query("priority"),
		Keyword:   strings.TrimSpace(c.Query("keyword")),
		Unread:    c.Query("unread") == "true",
		Unreplied: c.Query("unreplied") == "true",
		Offset:    pageInfo.GetStartIdx(),
		Limit:     pageInfo.GetPageSize(),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	items := make([]ticketResponse, 0, len(tickets))
	for _, ticket := range tickets {
		items = append(items, buildTicketResponse(&ticket, false))
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func AdminTicketCounts(c *gin.Context) {
	unread, unreplied, err := model.GetAdminTicketCounts()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, ticketCountResponse{UnreadCount: unread, UnrepliedCount: unreplied})
}

func AdminGetTicket(c *gin.Context) {
	ticket, err := getTicketFromParam(c)
	if err != nil {
		common.ApiErrorMsg(c, "invalid ticket id")
		return
	}
	if err := model.MarkTicketAdminRead(ticket.Id); err != nil {
		common.ApiError(c, err)
		return
	}
	response, err := loadTicketResponse(ticket, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, response)
}

func AdminReplyTicket(c *gin.Context) {
	ticket, err := getTicketFromParam(c)
	if err != nil {
		common.ApiErrorMsg(c, "invalid ticket id")
		return
	}
	_, _, _, content, images, err := parseTicketMultipart(c, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	_, _, _, content, err = model.ValidateTicketInput(ticket.Subject, ticket.Category, ticket.Priority, content)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	updatedTicket, _, err := model.AddTicketMessage(ticket.Id, c.GetInt("id"), model.TicketSenderAdmin, content, images)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	response, err := loadTicketResponse(updatedTicket, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, response)
}

func AdminUpdateTicketStatus(c *gin.Context) {
	ticket, err := getTicketFromParam(c)
	if err != nil {
		common.ApiErrorMsg(c, "invalid ticket id")
		return
	}
	var req ticketStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "invalid ticket status request")
		return
	}
	if err := model.UpdateTicketStatus(ticket.Id, strings.TrimSpace(req.Status)); err != nil {
		common.ApiError(c, err)
		return
	}
	ticket.Status = strings.TrimSpace(req.Status)
	common.ApiSuccess(c, buildTicketResponse(ticket, false))
}

func GetTicketImage(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "ticket image not found"})
		return
	}
	imageRecord, err := model.GetTicketImageById(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "ticket image not found"})
			return
		}
		common.ApiError(c, err)
		return
	}
	ticket, err := model.GetTicketById(imageRecord.TicketId)
	if err != nil || (ticket.UserId != c.GetInt("id") && c.GetInt("role") < common.RoleAdminUser) {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "ticket image not found"})
		return
	}
	c.Header("Cache-Control", "private, max-age=3600")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, imageRecord.MimeType, imageRecord.Data)
}

func getTicketFromParam(c *gin.Context) (*model.Ticket, error) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		return nil, errors.New("invalid ticket id")
	}
	return model.GetTicketById(id)
}

func buildTicketResponse(ticket *model.Ticket, viewerIsUser bool) ticketResponse {
	userName, _ := model.GetUsernameById(ticket.UserId, false)
	response := ticketResponse{
		Id:                    ticket.Id,
		UserId:                ticket.UserId,
		UserName:              userName,
		Subject:               ticket.Subject,
		Category:              ticket.Category,
		Priority:              ticket.Priority,
		Status:                ticket.Status,
		LastMessageSenderType: ticket.LastMessageSenderType,
		CreatedAt:             ticket.CreatedAt,
		UpdatedAt:             ticket.UpdatedAt,
		Unread:                ticket.LastMessageSenderType == model.TicketSenderUser && ticket.LastMessageId > ticket.AdminReadMessageId,
		Unreplied:             ticket.LastMessageSenderType == model.TicketSenderUser,
	}
	if viewerIsUser {
		response.Unread = false
		response.Unreplied = false
	}
	return response
}

func loadTicketResponse(ticket *model.Ticket, viewerIsUser bool) (*ticketResponse, error) {
	messages, err := model.GetTicketMessages(ticket.Id)
	if err != nil {
		return nil, err
	}
	messageIds := make([]int, 0, len(messages))
	for _, message := range messages {
		messageIds = append(messageIds, message.Id)
	}
	imagesByMessage, err := model.GetTicketImagesByMessageIds(messageIds)
	if err != nil {
		return nil, err
	}
	response := buildTicketResponse(ticket, viewerIsUser)
	response.Messages = make([]ticketMessageResponse, 0, len(messages))
	for _, message := range messages {
		images := make([]ticketImageResponse, 0, len(imagesByMessage[message.Id]))
		for _, image := range imagesByMessage[message.Id] {
			images = append(images, ticketImageResponse{
				Id:       image.Id,
				FileName: image.FileName,
				MimeType: image.MimeType,
				FileSize: image.FileSize,
			})
		}
		senderLabel := "User"
		if message.SenderType == model.TicketSenderAdmin {
			senderLabel = "Administrator"
		}
		response.Messages = append(response.Messages, ticketMessageResponse{
			Id:          message.Id,
			SenderType:  message.SenderType,
			SenderLabel: senderLabel,
			Content:     message.Content,
			Images:      images,
			CreatedAt:   message.CreatedAt,
		})
	}
	return &response, nil
}

func parseTicketMultipart(c *gin.Context, includeSubject bool) (string, string, string, string, []model.TicketImageInput, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, ticketMaxRequestBytes)
	if err := c.Request.ParseMultipartForm(ticketMaxRequestBytes); err != nil {
		return "", "", "", "", nil, fmt.Errorf("invalid ticket form: %w", err)
	}
	subject := c.PostForm("subject")
	category := c.PostForm("category")
	priority := c.PostForm("priority")
	content := c.PostForm("content")
	if !includeSubject {
		subject = "ticket reply"
		category = "other"
		priority = model.TicketPriorityNormal
	}
	files := c.Request.MultipartForm.File["images"]
	if len(files) > ticketMaxImageCount {
		return "", "", "", "", nil, fmt.Errorf("at most %d images are allowed", ticketMaxImageCount)
	}
	images := make([]model.TicketImageInput, 0, len(files))
	for _, header := range files {
		if header.Size > ticketMaxImageBytes {
			return "", "", "", "", nil, fmt.Errorf("image %s exceeds the %d MB limit", header.Filename, ticketMaxImageBytes/(1024*1024))
		}
		file, err := header.Open()
		if err != nil {
			return "", "", "", "", nil, fmt.Errorf("failed to read image %s", header.Filename)
		}
		data, readErr := io.ReadAll(io.LimitReader(file, ticketMaxImageBytes+1))
		_ = file.Close()
		if readErr != nil || int64(len(data)) > ticketMaxImageBytes {
			return "", "", "", "", nil, fmt.Errorf("failed to read image %s", header.Filename)
		}
		mimeType, err := validateTicketImage(data)
		if err != nil {
			return "", "", "", "", nil, fmt.Errorf("invalid image %s: %w", header.Filename, err)
		}
		images = append(images, model.TicketImageInput{
			FileName: filepath.Base(header.Filename),
			MimeType: mimeType,
			FileSize: int64(len(data)),
			Data:     data,
		})
	}
	return subject, category, priority, content, images, nil
}

func validateTicketImage(data []byte) (string, error) {
	if len(data) == 0 {
		return "", errors.New("image is empty")
	}
	_, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err == nil {
		switch format {
		case "png":
			return "image/png", nil
		case "jpeg":
			return "image/jpeg", nil
		case "gif":
			return "image/gif", nil
		}
	}
	if _, err := webp.DecodeConfig(bytes.NewReader(data)); err == nil {
		return "image/webp", nil
	}
	return "", errors.New("only PNG, JPEG, GIF, and WebP images are supported")
}
