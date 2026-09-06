package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	TicketStatusOpen       = "open"
	TicketStatusProcessing = "processing"
	TicketStatusResolved   = "resolved"
	TicketStatusClosed     = "closed"

	TicketPriorityNormal = "normal"
	TicketPriorityHigh   = "high"
	TicketPriorityUrgent = "urgent"

	TicketSenderUser  = "user"
	TicketSenderAdmin = "admin"

	TicketMaxSubjectLength = 120
	TicketMaxContentLength = 20000
)

var ticketStatuses = map[string]struct{}{
	TicketStatusOpen:       {},
	TicketStatusProcessing: {},
	TicketStatusResolved:   {},
	TicketStatusClosed:     {},
}

var ticketPriorities = map[string]struct{}{
	TicketPriorityNormal: {},
	TicketPriorityHigh:   {},
	TicketPriorityUrgent: {},
}

type Ticket struct {
	Id                    int    `json:"id" gorm:"primaryKey;autoIncrement"`
	UserId                int    `json:"user_id" gorm:"not null;index"`
	Subject               string `json:"subject" gorm:"type:varchar(120);not null"`
	Category              string `json:"category" gorm:"type:varchar(32);not null;index"`
	Priority              string `json:"priority" gorm:"type:varchar(16);not null;index"`
	Status                string `json:"status" gorm:"type:varchar(16);not null;index"`
	LastMessageId         int    `json:"last_message_id" gorm:"index"`
	LastMessageSenderType string `json:"last_message_sender_type" gorm:"type:varchar(16);index"`
	AdminReadMessageId    int    `json:"-"`
	UserReadMessageId     int    `json:"-"`
	CreatedAt             int64  `json:"created_at" gorm:"bigint;index"`
	UpdatedAt             int64  `json:"updated_at" gorm:"bigint;index"`
}

type TicketMessage struct {
	Id           int    `json:"id" gorm:"primaryKey;autoIncrement"`
	TicketId     int    `json:"ticket_id" gorm:"not null;index"`
	SenderUserId int    `json:"sender_user_id" gorm:"not null;index"`
	SenderType   string `json:"sender_type" gorm:"type:varchar(16);not null;index"`
	Content      string `json:"content" gorm:"type:text;not null"`
	CreatedAt    int64  `json:"created_at" gorm:"bigint;index"`
}

type TicketImage struct {
	Id         int    `json:"id" gorm:"primaryKey;autoIncrement"`
	TicketId   int    `json:"ticket_id" gorm:"not null;index"`
	MessageId  int    `json:"message_id" gorm:"not null;index"`
	UploaderId int    `json:"uploader_id" gorm:"not null;index"`
	FileName   string `json:"file_name" gorm:"type:varchar(255);not null"`
	MimeType   string `json:"mime_type" gorm:"type:varchar(64);not null"`
	FileSize   int64  `json:"file_size" gorm:"not null"`
	Data       []byte `json:"-" gorm:"not null"`
	CreatedAt  int64  `json:"created_at" gorm:"bigint;index"`
}

type TicketImageInput struct {
	FileName string
	MimeType string
	FileSize int64
	Data     []byte
}

type TicketListQuery struct {
	UserId    int
	Status    string
	Category  string
	Priority  string
	Keyword   string
	Unread    bool
	Unreplied bool
	Offset    int
	Limit     int
}

func (Ticket) TableName() string {
	return "tickets"
}

func (TicketMessage) TableName() string {
	return "ticket_messages"
}

func (TicketImage) TableName() string {
	return "ticket_images"
}

func ValidateTicketInput(subject, category, priority, content string) (string, string, string, string, error) {
	subject = strings.TrimSpace(subject)
	category = strings.TrimSpace(category)
	priority = strings.TrimSpace(priority)
	content = strings.TrimSpace(content)
	if subject == "" {
		return "", "", "", "", errors.New("ticket subject is required")
	}
	if len([]rune(subject)) > TicketMaxSubjectLength {
		return "", "", "", "", fmt.Errorf("ticket subject must be at most %d characters", TicketMaxSubjectLength)
	}
	if content == "" {
		return "", "", "", "", errors.New("ticket content is required")
	}
	if len([]rune(content)) > TicketMaxContentLength {
		return "", "", "", "", fmt.Errorf("ticket content must be at most %d characters", TicketMaxContentLength)
	}
	if category == "" {
		category = "other"
	}
	if priority == "" {
		priority = TicketPriorityNormal
	}
	if _, ok := ticketPriorities[priority]; !ok {
		return "", "", "", "", errors.New("invalid ticket priority")
	}
	return subject, category, priority, content, nil
}

func ValidateTicketStatus(status string) error {
	if _, ok := ticketStatuses[strings.TrimSpace(status)]; !ok {
		return errors.New("invalid ticket status")
	}
	return nil
}

func CreateTicket(userId int, subject, category, priority, content string, images []TicketImageInput) (*Ticket, error) {
	now := common.GetTimestamp()
	ticket := &Ticket{
		UserId:                userId,
		Subject:               subject,
		Category:              category,
		Priority:              priority,
		Status:                TicketStatusOpen,
		LastMessageSenderType: TicketSenderUser,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(ticket).Error; err != nil {
			return err
		}
		message := &TicketMessage{
			TicketId:     ticket.Id,
			SenderUserId: userId,
			SenderType:   TicketSenderUser,
			Content:      content,
			CreatedAt:    now,
		}
		if err := tx.Create(message).Error; err != nil {
			return err
		}
		if err := createTicketImages(tx, ticket.Id, message.Id, userId, images, now); err != nil {
			return err
		}
		ticket.LastMessageId = message.Id
		return tx.Model(ticket).Updates(map[string]interface{}{
			"last_message_id":          message.Id,
			"last_message_sender_type": TicketSenderUser,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return ticket, nil
}

func AddTicketMessage(ticketId, senderUserId int, senderType, content string, images []TicketImageInput) (*Ticket, *TicketMessage, error) {
	now := common.GetTimestamp()
	var ticket Ticket
	var message TicketMessage
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ?", ticketId).First(&ticket).Error; err != nil {
			return err
		}
		message = TicketMessage{
			TicketId:     ticket.Id,
			SenderUserId: senderUserId,
			SenderType:   senderType,
			Content:      content,
			CreatedAt:    now,
		}
		if err := tx.Create(&message).Error; err != nil {
			return err
		}
		if err := createTicketImages(tx, ticket.Id, message.Id, senderUserId, images, now); err != nil {
			return err
		}
		updates := map[string]interface{}{
			"last_message_id":          message.Id,
			"last_message_sender_type": senderType,
			"updated_at":               now,
		}
		if senderType == TicketSenderAdmin {
			updates["status"] = TicketStatusProcessing
			updates["admin_read_message_id"] = message.Id
		} else if ticket.Status == TicketStatusResolved || ticket.Status == TicketStatusClosed {
			updates["status"] = TicketStatusOpen
		}
		if err := tx.Model(&ticket).Updates(updates).Error; err != nil {
			return err
		}
		ticket.LastMessageId = message.Id
		ticket.LastMessageSenderType = senderType
		ticket.UpdatedAt = now
		if status, ok := updates["status"].(string); ok {
			ticket.Status = status
		}
		if senderType == TicketSenderAdmin {
			ticket.AdminReadMessageId = message.Id
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return &ticket, &message, nil
}

func createTicketImages(tx *gorm.DB, ticketId, messageId, uploaderId int, images []TicketImageInput, now int64) error {
	for _, image := range images {
		if err := tx.Create(&TicketImage{
			TicketId:   ticketId,
			MessageId:  messageId,
			UploaderId: uploaderId,
			FileName:   image.FileName,
			MimeType:   image.MimeType,
			FileSize:   image.FileSize,
			Data:       image.Data,
			CreatedAt:  now,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

func ListTickets(params TicketListQuery) ([]Ticket, int64, error) {
	query := DB.Model(&Ticket{})
	if params.UserId > 0 {
		query = query.Where("user_id = ?", params.UserId)
	}
	if params.Status != "" {
		query = query.Where("status = ?", params.Status)
	}
	if params.Category != "" {
		query = query.Where("category = ?", params.Category)
	}
	if params.Priority != "" {
		query = query.Where("priority = ?", params.Priority)
	}
	if params.Keyword != "" {
		like := "%" + params.Keyword + "%"
		query = query.Where("subject LIKE ? OR category LIKE ?", like, like)
	}
	if params.Unread {
		query = query.Where("last_message_sender_type = ? AND last_message_id > admin_read_message_id", TicketSenderUser)
	}
	if params.Unreplied {
		query = query.Where("last_message_sender_type = ?", TicketSenderUser)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	limit := params.Limit
	if limit <= 0 {
		limit = common.ItemsPerPage
	}
	var tickets []Ticket
	err := query.Offset(params.Offset).Limit(limit).Order("updated_at desc, id desc").Find(&tickets).Error
	return tickets, total, err
}

func GetTicketById(id int) (*Ticket, error) {
	var ticket Ticket
	if err := DB.Where("id = ?", id).First(&ticket).Error; err != nil {
		return nil, err
	}
	return &ticket, nil
}

func GetTicketMessages(ticketId int) ([]TicketMessage, error) {
	var messages []TicketMessage
	err := DB.Where("ticket_id = ?", ticketId).Order("id asc").Find(&messages).Error
	return messages, err
}

func GetTicketImagesByMessageIds(messageIds []int) (map[int][]TicketImage, error) {
	imagesByMessage := make(map[int][]TicketImage)
	if len(messageIds) == 0 {
		return imagesByMessage, nil
	}
	var images []TicketImage
	err := DB.Model(&TicketImage{}).
		Select("id, ticket_id, message_id, uploader_id, file_name, mime_type, file_size, created_at").
		Where("message_id IN ?", messageIds).
		Order("id asc").Find(&images).Error
	if err != nil {
		return nil, err
	}
	for _, image := range images {
		imagesByMessage[image.MessageId] = append(imagesByMessage[image.MessageId], image)
	}
	return imagesByMessage, nil
}

func GetTicketImageById(id int) (*TicketImage, error) {
	var image TicketImage
	if err := DB.Where("id = ?", id).First(&image).Error; err != nil {
		return nil, err
	}
	return &image, nil
}

func MarkTicketAdminRead(ticketId int) error {
	return DB.Model(&Ticket{}).Where("id = ? AND last_message_id > admin_read_message_id", ticketId).
		Updates(map[string]interface{}{"admin_read_message_id": gorm.Expr("last_message_id")}).Error
}

func MarkTicketUserRead(ticketId, userId int) error {
	return DB.Model(&Ticket{}).Where("id = ? AND user_id = ? AND last_message_id > user_read_message_id", ticketId, userId).
		Updates(map[string]interface{}{"user_read_message_id": gorm.Expr("last_message_id")}).Error
}

func CloseTicket(ticketId, userId int) error {
	result := DB.Model(&Ticket{}).Where("id = ? AND user_id = ?", ticketId, userId).
		Updates(map[string]interface{}{"status": TicketStatusClosed, "updated_at": common.GetTimestamp()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func UpdateTicketStatus(ticketId int, status string) error {
	if err := ValidateTicketStatus(status); err != nil {
		return err
	}
	result := DB.Model(&Ticket{}).Where("id = ?", ticketId).
		Updates(map[string]interface{}{"status": status, "updated_at": common.GetTimestamp()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func GetAdminTicketCounts() (int64, int64, error) {
	var unread int64
	if err := DB.Model(&Ticket{}).
		Where("last_message_sender_type = ? AND last_message_id > admin_read_message_id", TicketSenderUser).
		Count(&unread).Error; err != nil {
		return 0, 0, err
	}
	var unreplied int64
	if err := DB.Model(&Ticket{}).Where("last_message_sender_type = ?", TicketSenderUser).Count(&unreplied).Error; err != nil {
		return 0, 0, err
	}
	return unread, unreplied, nil
}
