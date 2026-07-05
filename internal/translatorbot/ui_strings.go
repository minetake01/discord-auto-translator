package translatorbot

import "strings"

const (
	uiKeyViewOriginalLink = "viewOriginalLink"
	uiKeyOriginalMessage  = "originalMessage"
	uiKeyAlreadyOriginal  = "alreadyOriginal"
	uiKeyNotManaged       = "notManaged"
	uiKeyForwarded        = "forwarded"
)

var uiStrings = map[string]map[string]string{
	"en": {
		uiKeyViewOriginalLink: "Go to original message",
		uiKeyOriginalMessage:  "Original message",
		uiKeyAlreadyOriginal:  "This message is already the original.",
		uiKeyNotManaged:       "This message is not managed by the translation bot.",
		uiKeyForwarded:        "Forwarded",
	},
	"ja": {
		uiKeyViewOriginalLink: "原文メッセージへ移動",
		uiKeyOriginalMessage:  "引用元を見る",
		uiKeyAlreadyOriginal:  "このメッセージは原文です。",
		uiKeyNotManaged:       "このメッセージは翻訳ボットが管理していません。",
		uiKeyForwarded:        "転送済み",
	},
	"zh-CN": {
		uiKeyViewOriginalLink: "前往原文消息",
		uiKeyOriginalMessage:  "查看原消息",
		uiKeyAlreadyOriginal:  "此消息已是原文。",
		uiKeyNotManaged:       "此消息不受翻译机器人管理。",
		uiKeyForwarded:        "已转发",
	},
	"zh-TW": {
		uiKeyViewOriginalLink: "前往原文訊息",
		uiKeyOriginalMessage:  "查看原始訊息",
		uiKeyAlreadyOriginal:  "此訊息已是原文。",
		uiKeyNotManaged:       "此訊息不受翻譯機器人管理。",
		uiKeyForwarded:        "已轉發",
	},
	"ko": {
		uiKeyViewOriginalLink: "원문 메시지로 이동",
		uiKeyOriginalMessage:  "원문 보기",
		uiKeyAlreadyOriginal:  "이 메시지는 이미 원문입니다.",
		uiKeyNotManaged:       "이 메시지는 번역 봇이 관리하지 않습니다.",
		uiKeyForwarded:        "전달됨",
	},
	"fr": {
		uiKeyViewOriginalLink: "Aller au message original",
		uiKeyOriginalMessage:  "Voir le message original",
		uiKeyAlreadyOriginal:  "Ce message est déjà l'original.",
		uiKeyNotManaged:       "Ce message n'est pas géré par le bot de traduction.",
		uiKeyForwarded:        "Transféré",
	},
	"de": {
		uiKeyViewOriginalLink: "Zur Originalnachricht",
		uiKeyOriginalMessage:  "Originalnachricht anzeigen",
		uiKeyAlreadyOriginal:  "Diese Nachricht ist bereits das Original.",
		uiKeyNotManaged:       "Diese Nachricht wird vom Übersetzungsbot nicht verwaltet.",
		uiKeyForwarded:        "Weitergeleitet",
	},
	"es": {
		uiKeyViewOriginalLink: "Ir al mensaje original",
		uiKeyOriginalMessage:  "Ver mensaje original",
		uiKeyAlreadyOriginal:  "Este mensaje ya es el original.",
		uiKeyNotManaged:       "Este mensaje no está gestionado por el bot de traducción.",
		uiKeyForwarded:        "Reenviado",
	},
	"pt-BR": {
		uiKeyViewOriginalLink: "Ir para a mensagem original",
		uiKeyOriginalMessage:  "Ver mensagem original",
		uiKeyAlreadyOriginal:  "Esta mensagem já é o original.",
		uiKeyNotManaged:       "Esta mensagem não é gerenciada pelo bot de tradução.",
		uiKeyForwarded:        "Encaminhado",
	},
	"it": {
		uiKeyViewOriginalLink: "Vai al messaggio originale",
		uiKeyOriginalMessage:  "Visualizza messaggio originale",
		uiKeyAlreadyOriginal:  "Questo messaggio è già l'originale.",
		uiKeyNotManaged:       "Questo messaggio non è gestito dal bot di traduzione.",
		uiKeyForwarded:        "Inoltrato",
	},
	"id": {
		uiKeyViewOriginalLink: "Buka pesan asli",
		uiKeyOriginalMessage:  "Lihat pesan asli",
		uiKeyAlreadyOriginal:  "Pesan ini sudah merupakan pesan asli.",
		uiKeyNotManaged:       "Pesan ini tidak dikelola oleh bot terjemahan.",
		uiKeyForwarded:        "Diteruskan",
	},
	"th": {
		uiKeyViewOriginalLink: "ไปยังข้อความต้นฉบับ",
		uiKeyOriginalMessage:  "ดูข้อความต้นฉบับ",
		uiKeyAlreadyOriginal:  "ข้อความนี้เป็นข้อความต้นฉบับอยู่แล้ว",
		uiKeyNotManaged:       "ข้อความนี้ไม่ได้อยู่ภายใต้การจัดการของบอทแปลภาษา",
		uiKeyForwarded:        "ส่งต่อแล้ว",
	},
	"vi": {
		uiKeyViewOriginalLink: "Đi tới tin nhắn gốc",
		uiKeyOriginalMessage:  "Xem tin nhắn gốc",
		uiKeyAlreadyOriginal:  "Tin nhắn này đã là bản gốc.",
		uiKeyNotManaged:       "Tin nhắn này không được bot dịch quản lý.",
		uiKeyForwarded:        "Đã chuyển tiếp",
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
