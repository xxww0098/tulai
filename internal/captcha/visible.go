package captcha

import (
	"errors"

	"github.com/xxww0098/tulai/internal/cdp"
)

// ErrVisible 表示页面上正有验证码。出图应停下来等人，不要当成出图失败。
var ErrVisible = errors.New("页面出现验证码")

// Visible 只读页面上是否有验证码。读失败时带回错误，调用方不能把它当成「已经通过」。
func Visible(s *cdp.Session) (bool, error) {
	if s == nil || s.IsEgo() {
		return false, nil
	}
	out, err := s.Evaluate("/* tulai captcha:verification */ (" + verificationExpression + ")")
	if err != nil {
		return false, err
	}
	return readBlocked(out), nil
}

func readBlocked(out string) bool {
	var blocked bool
	if cdp.ParseJSValue(out, &blocked) != nil {
		return false
	}
	return blocked
}
