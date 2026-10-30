package agy

import "strings"

// Instruction 让主代理去调 image-generator，再等它出图。
// ref 非空是洗图：带上参考图的绝对路径。生图不传参考图。
func Instruction(user, ref string) string {
	user = strings.TrimSpace(user)
	ref = strings.TrimSpace(ref)
	if ref != "" {
		if user == "" {
			user = "按洗图要求处理参考图，保留主体"
		}
		return "Invoke the image-generator subagent to edit the reference image " + ref + ": " + user + ". Then call the wait tool repeatedly until the subagent reports the image is generated. Do not run commands. Finally reply with only the absolute path of the generated image."
	}
	if user == "" {
		user = "按要求生成一张图片"
	}
	return "Invoke the image-generator subagent to generate an image: " + user + ". Then call the wait tool repeatedly until the subagent reports the image is generated. Do not run commands. Finally reply with only the absolute path of the generated image."
}
