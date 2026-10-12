package model

import "regexp"

// Match the frontend's /^high\s+risk(?:\s|$)/i after trim, including JS whitespace.
const highRiskGroupSpace = `[\s\x{000B}\p{Zs}\x{2028}\x{2029}\x{FEFF}]`

var highRiskSessionGroup = regexp.MustCompile(`(?i)^` + highRiskGroupSpace + `*high` + highRiskGroupSpace + `+risk(?:` + highRiskGroupSpace + `|$)`)

// UserEditNeedsSessionRevocation covers the fields editable by an administrator.
func UserEditNeedsSessionRevocation(current, updated User, updatePassword bool) bool {
	return (updatePassword && current.Password != updated.Password) ||
		(current.Group != updated.Group && !highRiskSessionGroup.MatchString(current.Group) && !highRiskSessionGroup.MatchString(updated.Group))
}
