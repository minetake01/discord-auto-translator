package translatorbot

import (
	"fmt"
	"strings"
)

var glossaryFeedbackSuffixes = map[string]string{
	"ja":    "で用語を登録できます",
	"en":    "to register preferred terms",
	"zh-CN": "注册术语",
	"zh-TW": "註冊術語",
	"ko":    "으로 용어를 등록할 수 있습니다",
	"fr":    "pour enregistrer des termes",
	"de":    "um Begriffe zu registrieren",
	"es":    "para registrar términos",
	"pt-BR": "para registrar termos",
	"it":    "per registrare termini",
	"id":    "untuk mendaftarkan istilah",
	"th":    "เพื่อลงทะเบียนคำศัพท์",
	"vi":    "để đăng ký thuật ngữ",
}

func GlossaryFeedbackHint(targetLanguage, commandID string) string {
	suffix := glossaryFeedbackSuffixFor(targetLanguage)
	if commandID == "" {
		return "\n-# /add-glossary " + suffix
	}
	return fmt.Sprintf("\n-# </add-glossary:%s> %s", commandID, suffix)
}

func glossaryFeedbackSuffixFor(targetLanguage string) string {
	lang := normalizeLanguage(targetLanguage)
	if suffix, ok := glossaryFeedbackSuffixes[lang]; ok {
		return suffix
	}
	if base, _, ok := strings.Cut(lang, "-"); ok {
		if suffix, ok := glossaryFeedbackSuffixes[base]; ok {
			return suffix
		}
	}
	return glossaryFeedbackSuffixes["en"]
}
