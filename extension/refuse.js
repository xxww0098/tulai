// 豆包 / 千问 / Gemini 内容审核话术。只在这一处维护。
// 豆包网页常见「内容不符合规范」「审核不通过」。
// 千问网页和万相绿网常见「疑似敏感」「inappropriate content」「data inspection failed」「绿网」。
// Gemini 网页常见「我无法生成这类图片」「无法为您制作」「I can't generate images of」「can't create images of」。
// 调用方只把当前这条助手回复和短提示条传进来，不扫历史，也不扫用户自己的提示词。
;(function () {
  const REFUSAL = /内容不符合规范|内容不合规|审核不通过|审核未通过|内容审核|安全审核|内容违规|违反相关规定|违反平台|版权相关限制|版权限制|涉及版权|版权问题|版权保护|侵犯.{0,6}(?:版权|知识产权|肖像权)|图片无法生成|我无法生成|我无法制作|我不能生成|无法为[你您](?:生成|制作|创建)|不能为[你您](?:生成|制作|创建)|无法生成这类|无法生成你|无法生成该|无法生成这张|(?:暂时)?无法(?:为[你您])?(?:生成|创作|绘制|制作)(?:对应|相关|该|此|这类|这张|[你您]的?)|(?:暂时)?无法用于(?:本次)?(?:创作|生成|绘图|绘制)|(?:该|这张)图片(?:暂时)?无法(?:使用|处理|用于)|抱歉，我无法|包含敏感|敏感内容|疑似敏感|不适宜的内容|不适当的内容|不当内容|绿网|换个话题|换其他主题|换个主题|还没学会如何回答|无法回答这个问题|涉及违规|社区规范|inappropriate content|content security warning|data[_ ]inspection[_ ]failed|green network|violat(?:es?|ing) (?:our |the )?(?:content |safety |usage )?polic|can(?:'t|not) (?:generate|create) (?:this |the |that )?images?(?: of)?|unable to (?:generate|create)(?: (?:this |the |that )?images?(?: of)?)?|unsafe content|safety guidelines/i

  function match(text) {
    const s = String(text || '').replace(/\s+/g, ' ').trim()
    if (!s) return ''
    const found = s.match(REFUSAL)
    if (!found) return ''
    const at = found.index
    const start = Math.max(0, at - 12)
    const end = Math.min(s.length, at + found[0].length + 24)
    return s.slice(start, end)
  }

  globalThis.TULAIRefuse = { match }
})()
