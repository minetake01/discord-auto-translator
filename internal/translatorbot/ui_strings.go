package translatorbot

import "strings"

const (
	uiKeyViewOriginalLink = "viewOriginalLink"
	uiKeyAlreadyOriginal  = "alreadyOriginal"
	uiKeyNotManaged       = "notManaged"
)

var uiStrings = map[string]map[string]string{
	"en": {
		uiKeyViewOriginalLink: "Go to original message",
		uiKeyAlreadyOriginal:  "This message is already the original.",
		uiKeyNotManaged:       "This message is not managed by the translation bot.",
	},
	"ja": {
		uiKeyViewOriginalLink: "原文メッセージへ移動",
		uiKeyAlreadyOriginal:  "このメッセージは原文です。",
		uiKeyNotManaged:       "このメッセージは翻訳ボットが管理していません。",
	},
	"zh-CN": {
		uiKeyViewOriginalLink: "前往原文消息",
		uiKeyAlreadyOriginal:  "此消息已是原文。",
		uiKeyNotManaged:       "此消息不受翻译机器人管理。",
	},
	"zh-TW": {
		uiKeyViewOriginalLink: "前往原文訊息",
		uiKeyAlreadyOriginal:  "此訊息已是原文。",
		uiKeyNotManaged:       "此訊息不受翻譯機器人管理。",
	},
	"ko": {
		uiKeyViewOriginalLink: "원문 메시지로 이동",
		uiKeyAlreadyOriginal:  "이 메시지는 이미 원문입니다.",
		uiKeyNotManaged:       "이 메시지는 번역 봇이 관리하지 않습니다.",
	},
	"fr": {
		uiKeyViewOriginalLink: "Aller au message original",
		uiKeyAlreadyOriginal:  "Ce message est déjà l'original.",
		uiKeyNotManaged:       "Ce message n'est pas géré par le bot de traduction.",
	},
	"de": {
		uiKeyViewOriginalLink: "Zur Originalnachricht",
		uiKeyAlreadyOriginal:  "Diese Nachricht ist bereits das Original.",
		uiKeyNotManaged:       "Diese Nachricht wird vom Übersetzungsbot nicht verwaltet.",
	},
	"es": {
		uiKeyViewOriginalLink: "Ir al mensaje original",
		uiKeyAlreadyOriginal:  "Este mensaje ya es el original.",
		uiKeyNotManaged:       "Este mensaje no está gestionado por el bot de traducción.",
	},
	"pt-BR": {
		uiKeyViewOriginalLink: "Ir para a mensagem original",
		uiKeyAlreadyOriginal:  "Esta mensagem já é o original.",
		uiKeyNotManaged:       "Esta mensagem não é gerenciada pelo bot de tradução.",
	},
	"it": {
		uiKeyViewOriginalLink: "Vai al messaggio originale",
		uiKeyAlreadyOriginal:  "Questo messaggio è già l'originale.",
		uiKeyNotManaged:       "Questo messaggio non è gestito dal bot di traduzione.",
	},
	"id": {
		uiKeyViewOriginalLink: "Buka pesan asli",
		uiKeyAlreadyOriginal:  "Pesan ini sudah merupakan pesan asli.",
		uiKeyNotManaged:       "Pesan ini tidak dikelola oleh bot terjemahan.",
	},
	"th": {
		uiKeyViewOriginalLink: "ไปยังข้อความต้นฉบับ",
		uiKeyAlreadyOriginal:  "ข้อความนี้เป็นข้อความต้นฉบับอยู่แล้ว",
		uiKeyNotManaged:       "ข้อความนี้ไม่ได้อยู่ภายใต้การจัดการของบอทแปลภาษา",
	},
	"vi": {
		uiKeyViewOriginalLink: "Đi tới tin nhắn gốc",
		uiKeyAlreadyOriginal:  "Tin nhắn này đã là bản gốc.",
		uiKeyNotManaged:       "Tin nhắn này không được bot dịch quản lý.",
	},
}

func resolveUILanguage(language string) string {
	language = normalizeLanguage(language)
	if language == "" {
		return "en"
	}
	for code := range uiStrings {
		if strings.EqualFold(language, code) {
			return code
		}
	}
	primary := strings.SplitN(language, "-", 2)[0]
	for code := range uiStrings {
		if strings.EqualFold(primary, strings.SplitN(code, "-", 2)[0]) {
			return code
		}
	}
	return "en"
}

func localizedUIString(language, key string) string {
	lang := resolveUILanguage(language)
	if table, ok := uiStrings[lang]; ok {
		if s, ok := table[key]; ok {
			return s
		}
	}
	return uiStrings["en"][key]
}
