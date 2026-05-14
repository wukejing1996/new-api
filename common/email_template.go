package common

import (
	"fmt"
	"html/template"
	"strings"
)

const (
	EmailLanguageZh = "zh"
	EmailLanguageEn = "en"
)

type emailTemplateData struct {
	Lang              string
	Title             string
	BrandName         string
	BrandPrefix       string
	BrandSuffix       string
	Heading           string
	AutoNotice        string
	SupportNotice     string
	Greeting          string
	Intro             string
	CodeDigits        []string
	ExpireNotice      string
	IgnoreNotice      string
	AntiPhishingLabel string
	AntiPhishingCode  string
	SecurityNotice    string
	Thanks            string
	Team              string
	ResetLink         string
	ResetLinkText     string
	ResetCopyNotice   string
	Content           template.HTML
}

func ResolveEmailLanguage(acceptLanguage string) string {
	if acceptLanguage == "" {
		return EmailLanguageZh
	}
	for _, item := range strings.Split(acceptLanguage, ",") {
		tag := strings.ToLower(strings.TrimSpace(strings.Split(item, ";")[0]))
		switch {
		case tag == "en" || strings.HasPrefix(tag, "en-"):
			return EmailLanguageEn
		case tag == "zh" || strings.HasPrefix(tag, "zh-"):
			return EmailLanguageZh
		}
	}
	return EmailLanguageZh
}

func emailDisplayName() string {
	brandName := strings.TrimSpace(SystemName)
	if brandName == "" {
		return "CostRouter"
	}
	return brandName
}

func emailBrandParts(brandName string) (string, string) {
	brandName = strings.TrimSpace(brandName)
	if brandName == "" {
		brandName = emailDisplayName()
	}
	runes := []rune(brandName)
	if len(runes) > 4 {
		return string(runes[:4]), string(runes[4:])
	}
	return brandName, ""
}

func verificationCodeDigits(code string) []string {
	digits := make([]string, 6)
	for i := 0; i < len(digits); i++ {
		if i < len(code) {
			digits[i] = code[i : i+1]
		}
	}
	return digits
}

func newEmailTemplateData(lang string, title string) emailTemplateData {
	brandName := emailDisplayName()
	brandPrefix, brandSuffix := emailBrandParts(brandName)
	data := emailTemplateData{
		Lang:             lang,
		Title:            title,
		BrandName:        brandName,
		BrandPrefix:      brandPrefix,
		BrandSuffix:      brandSuffix,
		AntiPhishingCode: brandName,
	}
	if lang == EmailLanguageEn {
		data.AutoNotice = "Note: This email was sent automatically. Please do not reply directly."
		data.SupportNotice = fmt.Sprintf("If you have any questions, please contact %s support.", brandName)
		data.Greeting = "Dear user,"
		data.AntiPhishingLabel = "Anti-phishing code"
		data.SecurityNotice = "Please keep your email account secure to prevent unauthorized access."
		data.Thanks = "Thank you for your support"
		data.Team = fmt.Sprintf("%s Team", brandName)
		return data
	}
	data.AutoNotice = "提示：此邮件是系统自动发出，请不要直接回复本邮件。"
	data.SupportNotice = fmt.Sprintf("如果您有任何疑问，欢迎联系 %s 客服。", brandName)
	data.Greeting = "尊敬的用户："
	data.AntiPhishingLabel = "防钓鱼码"
	data.SecurityNotice = "请妥善保管您的邮箱，避免账号被他人盗用。"
	data.Thanks = "感谢您的支持"
	data.Team = fmt.Sprintf("%s 团队", brandName)
	return data
}

func BuildVerificationEmailSubject(lang string) string {
	if lang == EmailLanguageEn {
		return fmt.Sprintf("%s Email Verification Code", emailDisplayName())
	}
	return fmt.Sprintf("%s 邮箱验证码", emailDisplayName())
}

func BuildVerificationEmailContent(lang string, code string) (string, error) {
	data := newEmailTemplateData(lang, BuildVerificationEmailSubject(lang))
	data.CodeDigits = verificationCodeDigits(code)
	if lang == EmailLanguageEn {
		data.Heading = "Email verification code"
		data.Intro = "You are verifying your email address. Your verification code is"
		data.ExpireNotice = fmt.Sprintf("This code is valid for %d minutes. Please enter it as soon as possible.", VerificationValidMinutes)
		data.IgnoreNotice = "If you did not request this, please ignore this email. If you have any questions, contact official support."
		return renderEmailTemplate(verificationEmailTemplate, data)
	}
	data.Heading = "邮箱验证码"
	data.Intro = "您正在进行邮箱验证操作，验证码是"
	data.ExpireNotice = fmt.Sprintf("有效期为%d分钟，请尽快填写。", VerificationValidMinutes)
	data.IgnoreNotice = "如非本人操作，请忽略本邮件。如有问题，请及时联系官方客服。"
	return renderEmailTemplate(verificationEmailTemplate, data)
}

func BuildPasswordResetEmailSubject(lang string) string {
	if lang == EmailLanguageEn {
		return fmt.Sprintf("%s Password Reset", emailDisplayName())
	}
	return fmt.Sprintf("%s 密码重置", emailDisplayName())
}

func BuildPasswordResetEmailContent(lang string, resetLink string) (string, error) {
	data := newEmailTemplateData(lang, BuildPasswordResetEmailSubject(lang))
	data.ResetLink = resetLink
	if lang == EmailLanguageEn {
		data.Heading = "Password reset"
		data.Intro = "You requested a password reset. Click the button below to continue."
		data.ResetLinkText = "Reset password"
		data.ResetCopyNotice = "If the button does not work, copy and paste this link into your browser:"
		data.ExpireNotice = fmt.Sprintf("This reset link is valid for %d minutes.", VerificationValidMinutes)
		data.IgnoreNotice = "If you did not request this, please ignore this email. Your account remains unchanged."
		return renderEmailTemplate(passwordResetEmailTemplate, data)
	}
	data.Heading = "密码重置"
	data.Intro = "您正在进行密码重置操作，请点击下方按钮继续。"
	data.ResetLinkText = "重置密码"
	data.ResetCopyNotice = "如果按钮无法点击，请复制以下链接到浏览器打开："
	data.ExpireNotice = fmt.Sprintf("重置链接有效期为%d分钟。", VerificationValidMinutes)
	data.IgnoreNotice = "如非本人操作，请忽略本邮件，您的账号不会被修改。"
	return renderEmailTemplate(passwordResetEmailTemplate, data)
}

func BuildNotificationEmailContent(title string, content string) (string, error) {
	data := newEmailTemplateData(EmailLanguageZh, title)
	data.Heading = title
	data.Content = template.HTML(content)
	return renderEmailTemplate(notificationEmailTemplate, data)
}

func renderEmailTemplate(tmpl string, data emailTemplateData) (string, error) {
	parsed, err := template.New("email").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var builder strings.Builder
	if err := parsed.Execute(&builder, data); err != nil {
		return "", err
	}
	return builder.String(), nil
}

const emailTemplateHeader = `<!DOCTYPE html>
<html lang="{{.Lang}}">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>{{.Title}}</title>
</head>
<body style="margin:0; padding:0; background-color:#e5e5e5; font-family:Arial, Helvetica, sans-serif; color:#1f2933;">
  <table width="100%" cellpadding="0" cellspacing="0" border="0" style="background-color:#e5e5e5; padding:0; margin:0;">
    <tr>
      <td align="center">
        <table width="600" cellpadding="0" cellspacing="0" border="0" style="width:600px; max-width:600px; background-color:#ffffff;">
          <tr>
            <td align="center" style="background-color:#171d26; padding:18px 0;">
              <div style="display:inline-block; font-size:26px; font-weight:700; color:#ffffff; letter-spacing:0.2px;">
                <span style="color:#18b957;">{{.BrandPrefix}}</span><span style="color:#ffffff;">{{.BrandSuffix}}</span>
              </div>
            </td>
          </tr>
          <tr>
            <td style="padding:52px 32px 24px 32px;">
              <h2 style="margin:0 0 24px 0; font-size:18px; line-height:1.5; color:#1f2933; font-weight:700;">
                {{.Heading}}
              </h2>
              <p style="margin:0 0 8px 0; font-size:14px; line-height:1.7; color:#1f2933;">
                {{.AutoNotice}}<br />{{.SupportNotice}}
              </p>
              <div style="height:1px; background-color:#e5e7eb; margin:36px 0 24px 0;"></div>
              <p style="margin:0 0 26px 0; font-size:16px; line-height:1.7; color:#1f2933; font-weight:700;">
                {{.Greeting}}
              </p>`

const emailTemplateFooter = `
              <table cellpadding="0" cellspacing="0" border="0" style="margin:0 0 14px 0;">
                <tr>
                  <td style="background-color:#171d26; color:#ffffff; font-size:13px; line-height:1; padding:7px 8px; font-weight:700;">
                    {{.AntiPhishingLabel}}
                  </td>
                  <td style="border:1px solid #171d26; color:#171d26; font-size:13px; line-height:1; padding:6px 10px; min-width:52px;">
                    {{.AntiPhishingCode}}
                  </td>
                </tr>
              </table>
              <div style="height:1px; background-color:#e5e7eb; margin:14px 0 16px 0;"></div>
              <p style="margin:0 0 24px 0; font-size:14px; line-height:1.7; color:#374151;">
                {{.SecurityNotice}}
              </p>
              <p style="margin:0 0 24px 0; font-size:14px; line-height:1.7; color:#374151;">
                {{.Thanks}}
              </p>
              <p style="margin:0; font-size:14px; line-height:1.7; color:#374151;">
                {{.Team}}
              </p>
            </td>
          </tr>
        </table>
      </td>
    </tr>
  </table>
</body>
</html>`

const verificationEmailTemplate = emailTemplateHeader + `
              <p style="margin:0 0 14px 0; font-size:14px; line-height:1.7; color:#374151;">
                {{.Intro}}
              </p>
              <table cellpadding="0" cellspacing="0" border="0" style="margin:0 0 40px 0;">
                <tr>
                  {{range .CodeDigits}}
                  <td style="width:40px; height:56px; background-color:#f3f6f9; border-radius:6px; text-align:center; vertical-align:middle; font-size:24px; font-weight:700; color:#1f2933;">
                    {{.}}
                  </td>
                  <td style="width:10px;"></td>
                  {{end}}
                </tr>
              </table>
              <p style="margin:0 0 8px 0; font-size:14px; line-height:1.7; color:#1f2933;">
                {{.ExpireNotice}}
              </p>
              <p style="margin:0 0 48px 0; font-size:14px; line-height:1.7; color:#1f2933;">
                {{.IgnoreNotice}}
              </p>` + emailTemplateFooter

const passwordResetEmailTemplate = emailTemplateHeader + `
              <p style="margin:0 0 22px 0; font-size:14px; line-height:1.7; color:#374151;">
                {{.Intro}}
              </p>
              <table cellpadding="0" cellspacing="0" border="0" style="margin:0 0 28px 0;">
                <tr>
                  <td style="background-color:#18b957; border-radius:6px;">
                    <a href="{{.ResetLink}}" style="display:inline-block; padding:13px 18px; color:#ffffff; font-size:14px; line-height:1; font-weight:700; text-decoration:none;">
                      {{.ResetLinkText}}
                    </a>
                  </td>
                </tr>
              </table>
              <p style="margin:0 0 8px 0; font-size:14px; line-height:1.7; color:#374151;">
                {{.ResetCopyNotice}}
              </p>
              <p style="margin:0 0 24px 0; font-size:13px; line-height:1.7; color:#374151; word-break:break-all;">
                <a href="{{.ResetLink}}" style="color:#171d26;">{{.ResetLink}}</a>
              </p>
              <p style="margin:0 0 8px 0; font-size:14px; line-height:1.7; color:#1f2933;">
                {{.ExpireNotice}}
              </p>
              <p style="margin:0 0 48px 0; font-size:14px; line-height:1.7; color:#1f2933;">
                {{.IgnoreNotice}}
              </p>` + emailTemplateFooter

const notificationEmailTemplate = emailTemplateHeader + `
              <div style="margin:0 0 48px 0; font-size:14px; line-height:1.7; color:#374151;">
                {{.Content}}
              </div>` + emailTemplateFooter
