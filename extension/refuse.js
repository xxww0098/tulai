// 豆包 / 千问内容审核话术。只在这一处维护。
// 豆包网页常见「内容不符合规范」「审核不通过」。
// 千问网页和万相绿网常见「疑似敏感」「inappropriate content」「data inspection failed」「绿网」。
// 调用方只把当前这条助手回复和短提示条传进来，不扫历史，也不扫用户自己的提示词。
;(function () {
  const REFUSAL = /内容不符合规范|内容不合规|审核不通过|审核未通过|内容审核|内容违规|违反相关规定|违反平台|我无法生成|无法为[你您]生成|不能为[你您]生成|无法生成这类|无法生成你|无法生成该|抱歉，我无法|包含敏感|敏感内容|疑似敏感|不适宜的内容|不适当的内容|不当内容|绿网|换个话题|还没学会如何回答|无法回答这个问题|涉及违规|社区规范|inappropriate content|data[_ ]inspection[_ ]failed|green network/i

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
