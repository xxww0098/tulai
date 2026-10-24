package captcha

import (
	"context"
	"errors"
	"fmt"
)

// ErrVerificationRequired 表示必须交还原页面，由用户完成验证。
var ErrVerificationRequired = errors.New("需人工验证：请在原网页手动完成验证后再开始任务")

const verificationExpression = `(function () {
  if (!window.__xbsCaptcha || typeof window.__xbsCaptcha.detect !== 'function') return false;
  var el = window.__xbsCaptcha.detect();
  if (!el) return false;
  var rect = el.getBoundingClientRect(), style = getComputedStyle(el);
  return rect.width > 0 && rect.height > 0 && style.display !== 'none' && style.visibility !== 'hidden' && style.opacity !== '0';
})()`

type verificationPage interface {
	Ctx() context.Context
	EgoEvaluate(string, any) error
}

// CheckVerificationRequired 只读取已安装监听的检测结果，不操作验证页面。
func CheckVerificationRequired(s verificationPage) error {
	if err := s.Ctx().Err(); err != nil {
		return err
	}
	var current struct {
		Blocked   bool `json:"blocked"`
		AutoSolve bool `json:"autoSolve"`
	}
	err := s.EgoEvaluate("/* xbs captcha:verification */ ({blocked:"+verificationExpression+`,autoSolve:!!(window.__xbsCaptcha && window.__xbsCaptcha.status && window.__xbsCaptcha.status().autoSolve)})`, &current)
	if ctxErr := s.Ctx().Err(); ctxErr != nil {
		return ctxErr
	}
	if err != nil {
		return fmt.Errorf("检查人工验证: %w", err)
	}
	if current.Blocked && !current.AutoSolve {
		return errors.Join(ErrVerificationRequired, SetManual(s))
	}
	return nil
}

// SetManual 阻止已发起求解在异步返回后继续动作，保留验证页面。
func SetManual(s verificationPage) error {
	if err := s.Ctx().Err(); err != nil {
		return err
	}
	err := s.EgoEvaluate(`/* xbs captcha:manual */ (window.__xbsCaptcha && window.__xbsCaptcha.setManual(true),true)`, nil)
	if ctxErr := s.Ctx().Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}
