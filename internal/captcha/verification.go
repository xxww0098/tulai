package captcha

import "errors"

// ErrVerificationRequired 表示必须交还原页面，由用户完成验证。
var ErrVerificationRequired = errors.New("需人工验证：请在原网页手动完成验证后再开始任务")

const verificationExpression = `(function () {
  if (!window.__tulaiCaptcha || typeof window.__tulaiCaptcha.detect !== 'function') return false;
  var el = window.__tulaiCaptcha.detect();
  if (!el) return false;
  var rect = el.getBoundingClientRect(), style = getComputedStyle(el);
  return rect.width > 0 && rect.height > 0 && style.display !== 'none' && style.visibility !== 'hidden' && style.opacity !== '0';
})()`
