package provider

import "github.com/wchiway/ocs_bank/internal/question"

func generationPrompt(images []question.Image) string {
	if len(images) == 0 {
		return prompt
	}
	return prompt + ` Attached images correspond, in order, to [图片1], [图片2], and so on. Use the images to answer the question. Preserve image markers exactly when returning option content. Never invent missing image content.`
}
