package translatorbot

import (
	"fmt"
	"strings"
)

// uiKey identifies one user-facing string in the localization catalog.
type uiKey string

const (
	uiKeyViewOriginalLink       uiKey = "viewOriginalLink"
	uiKeyOriginalMessage        uiKey = "originalMessage"
	uiKeyOriginalMessageDeleted uiKey = "originalMessageDeleted"
	uiKeyAlreadyOriginal        uiKey = "alreadyOriginal"
	uiKeyNotManaged             uiKey = "notManaged"
	uiKeyForwarded              uiKey = "forwarded"

	uiKeyRateLimitNotice         uiKey = "rateLimitNotice"
	uiKeyTranslationFailedNotice uiKey = "translationFailedNotice"

	uiKeyUnexpectedError        uiKey = "unexpectedError"
	uiKeyInvalidLanguage        uiKey = "invalidLanguage"
	uiKeyChannelFetchFailed     uiKey = "channelFetchFailed"
	uiKeyUnsupportedChannelType uiKey = "unsupportedChannelType"
	uiKeyWebhookCreateFailed    uiKey = "webhookCreateFailed"

	uiKeyGroupRequired     uiKey = "groupRequired"
	uiKeyGroupNotFound     uiKey = "groupNotFound"
	uiKeyJoinGroupNotFound uiKey = "joinGroupNotFound"
	uiKeyDuplicateGroup    uiKey = "duplicateGroup"
	uiKeyDuplicateChannel  uiKey = "duplicateChannel"
	uiKeyDuplicateLanguage uiKey = "duplicateLanguage"
	uiKeyChannelNotJoined  uiKey = "channelNotJoined"
	uiKeyChannelRegistered uiKey = "channelRegistered"
	uiKeyChannelJoined     uiKey = "channelJoined"
	uiKeyChannelLeft       uiKey = "channelLeft"
	uiKeyGroupDeleted      uiKey = "groupDeleted"

	uiKeyNoGroups        uiKey = "noGroups"
	uiKeyGroupsHeader    uiKey = "groupsHeader"
	uiKeyGroupsTruncated uiKey = "groupsTruncated"
	uiKeyGroupNoChannels uiKey = "groupNoChannels"

	uiKeyGlossaryTermRequired   uiKey = "glossaryTermRequired"
	uiKeyGlossaryFull           uiKey = "glossaryFull"
	uiKeyGlossaryAdded          uiKey = "glossaryAdded"
	uiKeyGlossaryAttributeNone  uiKey = "glossaryAttributeNone"
	uiKeyGlossaryAttributeLabel uiKey = "glossaryAttributeLabel"
	uiKeyGlossaryModeAlways     uiKey = "glossaryModeAlways"
	uiKeyGlossaryModeMatched    uiKey = "glossaryModeMatched"
	uiKeyGlossaryRemoved        uiKey = "glossaryRemoved"
	uiKeyGlossaryNotFound       uiKey = "glossaryNotFound"
	uiKeyNoGlossary             uiKey = "noGlossary"
	uiKeyGlossaryHeader         uiKey = "glossaryHeader"
	uiKeyGlossaryTruncated      uiKey = "glossaryTruncated"

	uiKeyStyleBothSpecified uiKey = "styleBothSpecified"
	uiKeyStyleNoneSpecified uiKey = "styleNoneSpecified"
	uiKeyStyleUnknownPreset uiKey = "styleUnknownPreset"
	uiKeyStyleCustomEmpty   uiKey = "styleCustomEmpty"
	uiKeyStyleCustomTooLong uiKey = "styleCustomTooLong"
	uiKeyStyleCustomSet     uiKey = "styleCustomSet"
	uiKeyStylePresetSet     uiKey = "stylePresetSet"
	uiKeyStyleReset         uiKey = "styleReset"
)

// uiStrings maps a supported UI language to its full message catalog.
// Every language must define every key; TestUIStringCatalogIsComplete
// enforces this together with matching format verbs.
var uiStrings = map[string]map[uiKey]string{
	"en": {
		uiKeyViewOriginalLink:       "Go to original message",
		uiKeyOriginalMessage:        "Source",
		uiKeyOriginalMessageDeleted: "The original message was deleted",
		uiKeyAlreadyOriginal:        "This message is already the original.",
		uiKeyNotManaged:             "This message is not managed by the translation bot.",
		uiKeyForwarded:              "Forwarded",

		uiKeyRateLimitNotice:         "This message was not translated because the translation rate limit was reached.",
		uiKeyTranslationFailedNotice: "This message was not mirrored because translation failed.",

		uiKeyUnexpectedError:        "An internal error occurred. Please try again later.",
		uiKeyInvalidLanguage:        "Specify the language as a short BCP-47 code such as `en`, `ja`, `zh-CN`, or `pt-BR`.",
		uiKeyChannelFetchFailed:     "Could not fetch the channel.",
		uiKeyUnsupportedChannelType: "Only text, announcement, forum, and media channels are supported.",
		uiKeyWebhookCreateFailed:    "Could not create a webhook.",

		uiKeyGroupRequired:     "Specify a group name.",
		uiKeyGroupNotFound:     "Translation group `%[1]s` was not found in this server.",
		uiKeyJoinGroupNotFound: "Translation group `%[1]s` was not found in this server. Make sure it matches a group created with `/new-channel`.",
		uiKeyDuplicateGroup:    "Translation group `%[1]s` already exists in this server.",
		uiKeyDuplicateChannel:  "This channel has already joined group `%[1]s`.",
		uiKeyDuplicateLanguage: "Group `%[1]s` already has a channel with the same language. Specify a different language.",
		uiKeyChannelNotJoined:  "This channel has not joined group `%[1]s`.",
		uiKeyChannelRegistered: "Registered <#%[2]s> (%[3]s) to translation group `%[1]s`.",
		uiKeyChannelJoined:     "Joined <#%[2]s> (%[3]s) to translation group `%[1]s`.",
		uiKeyChannelLeft:       "Removed <#%[2]s> from translation group `%[1]s`.",
		uiKeyGroupDeleted:      "Deleted translation group `%[1]s`.",

		uiKeyNoGroups:        "No translation groups are registered in this server.",
		uiKeyGroupsHeader:    "Translation groups (%[1]d):",
		uiKeyGroupsTruncated: "\n(remaining groups omitted)",
		uiKeyGroupNoChannels: "  (no channels)",

		uiKeyGlossaryTermRequired:   "Specify both a term and a translation.",
		uiKeyGlossaryFull:           "The glossary for this server is full (max %[1]d entries).",
		uiKeyGlossaryAdded:          "Registered term `%[1]s` as `%[2]s` (%[3]s, %[4]s).",
		uiKeyGlossaryAttributeNone:  "no attribute",
		uiKeyGlossaryAttributeLabel: "attribute: %[1]s",
		uiKeyGlossaryModeAlways:     "always applied",
		uiKeyGlossaryModeMatched:    "applied when the message contains the term",
		uiKeyGlossaryRemoved:        "Removed term `%[1]s`.",
		uiKeyGlossaryNotFound:       "Term `%[1]s` is not registered.",
		uiKeyNoGlossary:             "No glossary entries are registered in this server.",
		uiKeyGlossaryHeader:         "Glossary (%[1]d):",
		uiKeyGlossaryTruncated:      "\n(remaining entries omitted)",

		uiKeyStyleBothSpecified: "You cannot specify both a preset and a custom instruction. Specify only one.",
		uiKeyStyleNoneSpecified: "Specify either a preset or a custom instruction. To reset the style, specify `preset:default`.",
		uiKeyStyleUnknownPreset: "Unknown preset. Choose one of the command options.",
		uiKeyStyleCustomEmpty:   "The custom instruction cannot be empty.",
		uiKeyStyleCustomTooLong: "The custom instruction must be at most %[1]d characters.",
		uiKeyStyleCustomSet:     "Set the style of translation group `%[1]s` to a custom instruction: `%[2]s`",
		uiKeyStylePresetSet:     "Set the style of translation group `%[1]s` to preset `%[2]s`.",
		uiKeyStyleReset:         "Reset the style of translation group `%[1]s`.",
	},
	"ja": {
		uiKeyViewOriginalLink:       "原文メッセージへ移動",
		uiKeyOriginalMessage:        "引用元を見る",
		uiKeyOriginalMessageDeleted: "元のメッセージが削除されました",
		uiKeyAlreadyOriginal:        "このメッセージは原文です。",
		uiKeyNotManaged:             "このメッセージは翻訳ボットが管理していません。",
		uiKeyForwarded:              "転送済み",

		uiKeyRateLimitNotice:         "翻訳レート制限に達したため、このメッセージは翻訳されませんでした。",
		uiKeyTranslationFailedNotice: "翻訳に失敗したため、このメッセージはミラーリングされませんでした。",

		uiKeyUnexpectedError:        "内部エラーが発生しました。時間をおいて再度お試しください。",
		uiKeyInvalidLanguage:        "言語は `en`, `ja`, `zh-CN`, `pt-BR` のようなBCP-47形式の短いコードで指定してください。",
		uiKeyChannelFetchFailed:     "チャンネルを取得できませんでした。",
		uiKeyUnsupportedChannelType: "テキスト、アナウンス、フォーラム、メディアチャンネルだけ登録できます。",
		uiKeyWebhookCreateFailed:    "Webhookを作成できませんでした。",

		uiKeyGroupRequired:     "グループ名を指定してください。",
		uiKeyGroupNotFound:     "翻訳グループ `%[1]s` がこのサーバーに見つかりません。",
		uiKeyJoinGroupNotFound: "翻訳グループ `%[1]s` がこのサーバーに見つかりません。`/new-channel` で作成したグループ名と一致しているか確認してください。",
		uiKeyDuplicateGroup:    "翻訳グループ `%[1]s` は既にこのサーバーに存在します。",
		uiKeyDuplicateChannel:  "このチャンネルは既にグループ `%[1]s` に参加しています。",
		uiKeyDuplicateLanguage: "グループ `%[1]s` には既に同じ言語のチャンネルがあります。別の言語を指定してください。",
		uiKeyChannelNotJoined:  "このチャンネルはグループ `%[1]s` に参加していません。",
		uiKeyChannelRegistered: "翻訳グループ `%[1]s` に <#%[2]s> (%[3]s) を登録しました。",
		uiKeyChannelJoined:     "翻訳グループ `%[1]s` に <#%[2]s> (%[3]s) を参加させました。",
		uiKeyChannelLeft:       "翻訳グループ `%[1]s` から <#%[2]s> を退出させました。",
		uiKeyGroupDeleted:      "翻訳グループ `%[1]s` を削除しました。",

		uiKeyNoGroups:        "このサーバーには翻訳グループが登録されていません。",
		uiKeyGroupsHeader:    "翻訳グループ (%[1]d):",
		uiKeyGroupsTruncated: "\n（残りのグループは表示を省略しました）",
		uiKeyGroupNoChannels: "  （チャンネルなし）",

		uiKeyGlossaryTermRequired:   "用語と訳の両方を指定してください。",
		uiKeyGlossaryFull:           "このサーバーの用語集は上限（%[1]d件）に達しています。",
		uiKeyGlossaryAdded:          "用語 `%[1]s` を `%[2]s` として登録しました（%[3]s、%[4]s）。",
		uiKeyGlossaryAttributeNone:  "属性なし",
		uiKeyGlossaryAttributeLabel: "属性: %[1]s",
		uiKeyGlossaryModeAlways:     "常に使用",
		uiKeyGlossaryModeMatched:    "本文一致時のみ使用",
		uiKeyGlossaryRemoved:        "用語 `%[1]s` を削除しました。",
		uiKeyGlossaryNotFound:       "用語 `%[1]s` は登録されていません。",
		uiKeyNoGlossary:             "このサーバーには用語集の登録がありません。",
		uiKeyGlossaryHeader:         "用語集 (%[1]d):",
		uiKeyGlossaryTruncated:      "\n（残りの用語は表示を省略しました）",

		uiKeyStyleBothSpecified: "プリセットとカスタム指示は同時に指定できません。どちらか一方だけ指定してください。",
		uiKeyStyleNoneSpecified: "プリセットまたはカスタム指示のどちらかを指定してください。スタイルをリセットする場合は `preset:default` を指定してください。",
		uiKeyStyleUnknownPreset: "不明なプリセットです。コマンドの選択肢から指定してください。",
		uiKeyStyleCustomEmpty:   "カスタム指示は空にできません。",
		uiKeyStyleCustomTooLong: "カスタム指示は%[1]d文字以内で指定してください。",
		uiKeyStyleCustomSet:     "翻訳グループ `%[1]s` のスタイルをカスタム指示に設定しました: `%[2]s`",
		uiKeyStylePresetSet:     "翻訳グループ `%[1]s` のスタイルをプリセット `%[2]s` に設定しました。",
		uiKeyStyleReset:         "翻訳グループ `%[1]s` のスタイルをリセットしました。",
	},
	"zh-CN": {
		uiKeyViewOriginalLink:       "前往原文消息",
		uiKeyOriginalMessage:        "查看来源",
		uiKeyOriginalMessageDeleted: "原消息已被删除",
		uiKeyAlreadyOriginal:        "此消息已是原文。",
		uiKeyNotManaged:             "此消息不受翻译机器人管理。",
		uiKeyForwarded:              "已转发",

		uiKeyRateLimitNotice:         "已达到翻译速率限制，此消息未被翻译。",
		uiKeyTranslationFailedNotice: "翻译失败，此消息未被镜像。",

		uiKeyUnexpectedError:        "发生内部错误，请稍后重试。",
		uiKeyInvalidLanguage:        "请使用 BCP-47 格式的简短语言代码，例如 `en`、`ja`、`zh-CN`、`pt-BR`。",
		uiKeyChannelFetchFailed:     "无法获取频道。",
		uiKeyUnsupportedChannelType: "仅支持文字、公告、论坛和媒体频道。",
		uiKeyWebhookCreateFailed:    "无法创建 Webhook。",

		uiKeyGroupRequired:     "请指定组名。",
		uiKeyGroupNotFound:     "在此服务器中找不到翻译组 `%[1]s`。",
		uiKeyJoinGroupNotFound: "在此服务器中找不到翻译组 `%[1]s`。请确认它与使用 `/new-channel` 创建的组名一致。",
		uiKeyDuplicateGroup:    "翻译组 `%[1]s` 已存在于此服务器。",
		uiKeyDuplicateChannel:  "此频道已加入组 `%[1]s`。",
		uiKeyDuplicateLanguage: "组 `%[1]s` 中已存在相同语言的频道。请指定其他语言。",
		uiKeyChannelNotJoined:  "此频道未加入组 `%[1]s`。",
		uiKeyChannelRegistered: "已将 <#%[2]s> (%[3]s) 注册到翻译组 `%[1]s`。",
		uiKeyChannelJoined:     "已将 <#%[2]s> (%[3]s) 加入翻译组 `%[1]s`。",
		uiKeyChannelLeft:       "已将 <#%[2]s> 从翻译组 `%[1]s` 中移除。",
		uiKeyGroupDeleted:      "已删除翻译组 `%[1]s`。",

		uiKeyNoGroups:        "此服务器未注册任何翻译组。",
		uiKeyGroupsHeader:    "翻译组 (%[1]d):",
		uiKeyGroupsTruncated: "\n（其余组已省略）",
		uiKeyGroupNoChannels: "  （无频道）",

		uiKeyGlossaryTermRequired:   "请同时指定术语和译文。",
		uiKeyGlossaryFull:           "此服务器的术语表已满（最多 %[1]d 条）。",
		uiKeyGlossaryAdded:          "已将术语 `%[1]s` 注册为 `%[2]s`（%[3]s，%[4]s）。",
		uiKeyGlossaryAttributeNone:  "无属性",
		uiKeyGlossaryAttributeLabel: "属性: %[1]s",
		uiKeyGlossaryModeAlways:     "始终应用",
		uiKeyGlossaryModeMatched:    "仅在消息包含该术语时应用",
		uiKeyGlossaryRemoved:        "已删除术语 `%[1]s`。",
		uiKeyGlossaryNotFound:       "术语 `%[1]s` 未注册。",
		uiKeyNoGlossary:             "此服务器未注册任何术语表条目。",
		uiKeyGlossaryHeader:         "术语表 (%[1]d):",
		uiKeyGlossaryTruncated:      "\n（其余术语已省略）",

		uiKeyStyleBothSpecified: "不能同时指定预设和自定义指令。请只指定其中一个。",
		uiKeyStyleNoneSpecified: "请指定预设或自定义指令。要重置风格，请指定 `preset:default`。",
		uiKeyStyleUnknownPreset: "未知的预设。请从命令选项中选择。",
		uiKeyStyleCustomEmpty:   "自定义指令不能为空。",
		uiKeyStyleCustomTooLong: "自定义指令最多 %[1]d 个字符。",
		uiKeyStyleCustomSet:     "已将翻译组 `%[1]s` 的风格设置为自定义指令: `%[2]s`",
		uiKeyStylePresetSet:     "已将翻译组 `%[1]s` 的风格设置为预设 `%[2]s`。",
		uiKeyStyleReset:         "已重置翻译组 `%[1]s` 的风格。",
	},
	"zh-TW": {
		uiKeyViewOriginalLink:       "前往原文訊息",
		uiKeyOriginalMessage:        "查看來源",
		uiKeyOriginalMessageDeleted: "原訊息已被刪除",
		uiKeyAlreadyOriginal:        "此訊息已是原文。",
		uiKeyNotManaged:             "此訊息不受翻譯機器人管理。",
		uiKeyForwarded:              "已轉發",

		uiKeyRateLimitNotice:         "已達到翻譯速率限制，此訊息未被翻譯。",
		uiKeyTranslationFailedNotice: "翻譯失敗，此訊息未被鏡像。",

		uiKeyUnexpectedError:        "發生內部錯誤，請稍後再試。",
		uiKeyInvalidLanguage:        "請使用 BCP-47 格式的簡短語言代碼，例如 `en`、`ja`、`zh-CN`、`pt-BR`。",
		uiKeyChannelFetchFailed:     "無法取得頻道。",
		uiKeyUnsupportedChannelType: "僅支援文字、公告、論壇和媒體頻道。",
		uiKeyWebhookCreateFailed:    "無法建立 Webhook。",

		uiKeyGroupRequired:     "請指定群組名稱。",
		uiKeyGroupNotFound:     "在此伺服器中找不到翻譯群組 `%[1]s`。",
		uiKeyJoinGroupNotFound: "在此伺服器中找不到翻譯群組 `%[1]s`。請確認它與使用 `/new-channel` 建立的群組名稱一致。",
		uiKeyDuplicateGroup:    "翻譯群組 `%[1]s` 已存在於此伺服器。",
		uiKeyDuplicateChannel:  "此頻道已加入群組 `%[1]s`。",
		uiKeyDuplicateLanguage: "群組 `%[1]s` 中已存在相同語言的頻道。請指定其他語言。",
		uiKeyChannelNotJoined:  "此頻道未加入群組 `%[1]s`。",
		uiKeyChannelRegistered: "已將 <#%[2]s> (%[3]s) 註冊到翻譯群組 `%[1]s`。",
		uiKeyChannelJoined:     "已將 <#%[2]s> (%[3]s) 加入翻譯群組 `%[1]s`。",
		uiKeyChannelLeft:       "已將 <#%[2]s> 從翻譯群組 `%[1]s` 中移除。",
		uiKeyGroupDeleted:      "已刪除翻譯群組 `%[1]s`。",

		uiKeyNoGroups:        "此伺服器未註冊任何翻譯群組。",
		uiKeyGroupsHeader:    "翻譯群組 (%[1]d):",
		uiKeyGroupsTruncated: "\n（其餘群組已省略）",
		uiKeyGroupNoChannels: "  （無頻道）",

		uiKeyGlossaryTermRequired:   "請同時指定術語和譯文。",
		uiKeyGlossaryFull:           "此伺服器的詞彙表已滿（最多 %[1]d 筆）。",
		uiKeyGlossaryAdded:          "已將術語 `%[1]s` 註冊為 `%[2]s`（%[3]s，%[4]s）。",
		uiKeyGlossaryAttributeNone:  "無屬性",
		uiKeyGlossaryAttributeLabel: "屬性: %[1]s",
		uiKeyGlossaryModeAlways:     "始終套用",
		uiKeyGlossaryModeMatched:    "僅在訊息包含該術語時套用",
		uiKeyGlossaryRemoved:        "已刪除術語 `%[1]s`。",
		uiKeyGlossaryNotFound:       "術語 `%[1]s` 未註冊。",
		uiKeyNoGlossary:             "此伺服器未註冊任何詞彙表項目。",
		uiKeyGlossaryHeader:         "詞彙表 (%[1]d):",
		uiKeyGlossaryTruncated:      "\n（其餘術語已省略）",

		uiKeyStyleBothSpecified: "不能同時指定預設和自訂指令。請只指定其中一個。",
		uiKeyStyleNoneSpecified: "請指定預設或自訂指令。要重設風格，請指定 `preset:default`。",
		uiKeyStyleUnknownPreset: "未知的預設。請從指令選項中選擇。",
		uiKeyStyleCustomEmpty:   "自訂指令不能為空。",
		uiKeyStyleCustomTooLong: "自訂指令最多 %[1]d 個字元。",
		uiKeyStyleCustomSet:     "已將翻譯群組 `%[1]s` 的風格設定為自訂指令: `%[2]s`",
		uiKeyStylePresetSet:     "已將翻譯群組 `%[1]s` 的風格設定為預設 `%[2]s`。",
		uiKeyStyleReset:         "已重設翻譯群組 `%[1]s` 的風格。",
	},
	"ko": {
		uiKeyViewOriginalLink:       "원문 메시지로 이동",
		uiKeyOriginalMessage:        "출처 보기",
		uiKeyOriginalMessageDeleted: "원본 메시지가 삭제되었습니다",
		uiKeyAlreadyOriginal:        "이 메시지는 이미 원문입니다.",
		uiKeyNotManaged:             "이 메시지는 번역 봇이 관리하지 않습니다.",
		uiKeyForwarded:              "전달됨",

		uiKeyRateLimitNotice:         "번역 속도 제한에 도달하여 이 메시지는 번역되지 않았습니다.",
		uiKeyTranslationFailedNotice: "번역에 실패하여 이 메시지는 미러링되지 않았습니다.",

		uiKeyUnexpectedError:        "내부 오류가 발생했습니다. 잠시 후 다시 시도해 주세요.",
		uiKeyInvalidLanguage:        "언어는 `en`, `ja`, `zh-CN`, `pt-BR`와 같은 BCP-47 형식의 짧은 코드로 지정해 주세요.",
		uiKeyChannelFetchFailed:     "채널을 가져올 수 없습니다.",
		uiKeyUnsupportedChannelType: "텍스트, 공지, 포럼, 미디어 채널만 등록할 수 있습니다.",
		uiKeyWebhookCreateFailed:    "웹후크를 만들 수 없습니다.",

		uiKeyGroupRequired:     "그룹 이름을 지정해 주세요.",
		uiKeyGroupNotFound:     "이 서버에서 번역 그룹 `%[1]s`을(를) 찾을 수 없습니다.",
		uiKeyJoinGroupNotFound: "이 서버에서 번역 그룹 `%[1]s`을(를) 찾을 수 없습니다. `/new-channel`로 만든 그룹 이름과 일치하는지 확인해 주세요.",
		uiKeyDuplicateGroup:    "번역 그룹 `%[1]s`이(가) 이미 이 서버에 존재합니다.",
		uiKeyDuplicateChannel:  "이 채널은 이미 그룹 `%[1]s`에 참여하고 있습니다.",
		uiKeyDuplicateLanguage: "그룹 `%[1]s`에는 이미 같은 언어의 채널이 있습니다. 다른 언어를 지정해 주세요.",
		uiKeyChannelNotJoined:  "이 채널은 그룹 `%[1]s`에 참여하고 있지 않습니다.",
		uiKeyChannelRegistered: "번역 그룹 `%[1]s`에 <#%[2]s> (%[3]s)을(를) 등록했습니다.",
		uiKeyChannelJoined:     "번역 그룹 `%[1]s`에 <#%[2]s> (%[3]s)을(를) 참여시켰습니다.",
		uiKeyChannelLeft:       "번역 그룹 `%[1]s`에서 <#%[2]s>을(를) 제거했습니다.",
		uiKeyGroupDeleted:      "번역 그룹 `%[1]s`을(를) 삭제했습니다.",

		uiKeyNoGroups:        "이 서버에 등록된 번역 그룹이 없습니다.",
		uiKeyGroupsHeader:    "번역 그룹 (%[1]d):",
		uiKeyGroupsTruncated: "\n(나머지 그룹은 생략되었습니다)",
		uiKeyGroupNoChannels: "  (채널 없음)",

		uiKeyGlossaryTermRequired:   "용어와 번역을 모두 지정해 주세요.",
		uiKeyGlossaryFull:           "이 서버의 용어집이 가득 찼습니다(최대 %[1]d개).",
		uiKeyGlossaryAdded:          "용어 `%[1]s`을(를) `%[2]s`(으)로 등록했습니다 (%[3]s, %[4]s).",
		uiKeyGlossaryAttributeNone:  "속성 없음",
		uiKeyGlossaryAttributeLabel: "속성: %[1]s",
		uiKeyGlossaryModeAlways:     "항상 적용",
		uiKeyGlossaryModeMatched:    "메시지에 용어가 포함될 때만 적용",
		uiKeyGlossaryRemoved:        "용어 `%[1]s`을(를) 삭제했습니다.",
		uiKeyGlossaryNotFound:       "용어 `%[1]s`은(는) 등록되어 있지 않습니다.",
		uiKeyNoGlossary:             "이 서버에 등록된 용어집 항목이 없습니다.",
		uiKeyGlossaryHeader:         "용어집 (%[1]d):",
		uiKeyGlossaryTruncated:      "\n(나머지 용어는 생략되었습니다)",

		uiKeyStyleBothSpecified: "프리셋과 사용자 지정 지시를 동시에 지정할 수 없습니다. 하나만 지정해 주세요.",
		uiKeyStyleNoneSpecified: "프리셋 또는 사용자 지정 지시 중 하나를 지정해 주세요. 스타일을 초기화하려면 `preset:default`를 지정하세요.",
		uiKeyStyleUnknownPreset: "알 수 없는 프리셋입니다. 명령 선택지에서 지정해 주세요.",
		uiKeyStyleCustomEmpty:   "사용자 지정 지시는 비워 둘 수 없습니다.",
		uiKeyStyleCustomTooLong: "사용자 지정 지시는 %[1]d자 이내로 지정해 주세요.",
		uiKeyStyleCustomSet:     "번역 그룹 `%[1]s`의 스타일을 사용자 지정 지시로 설정했습니다: `%[2]s`",
		uiKeyStylePresetSet:     "번역 그룹 `%[1]s`의 스타일을 프리셋 `%[2]s`(으)로 설정했습니다.",
		uiKeyStyleReset:         "번역 그룹 `%[1]s`의 스타일을 초기화했습니다.",
	},
	"fr": {
		uiKeyViewOriginalLink:       "Aller au message original",
		uiKeyOriginalMessage:        "Source",
		uiKeyOriginalMessageDeleted: "Le message d’origine a été supprimé",
		uiKeyAlreadyOriginal:        "Ce message est déjà l'original.",
		uiKeyNotManaged:             "Ce message n'est pas géré par le bot de traduction.",
		uiKeyForwarded:              "Transféré",

		uiKeyRateLimitNotice:         "Ce message n'a pas été traduit car la limite de débit de traduction a été atteinte.",
		uiKeyTranslationFailedNotice: "Ce message n'a pas été miroité car la traduction a échoué.",

		uiKeyUnexpectedError:        "Une erreur interne s'est produite. Veuillez réessayer plus tard.",
		uiKeyInvalidLanguage:        "Indiquez la langue sous forme de code BCP-47 court, par exemple `en`, `ja`, `zh-CN` ou `pt-BR`.",
		uiKeyChannelFetchFailed:     "Impossible de récupérer le salon.",
		uiKeyUnsupportedChannelType: "Seuls les salons textuels, d'annonces, de forum et de médias sont pris en charge.",
		uiKeyWebhookCreateFailed:    "Impossible de créer un webhook.",

		uiKeyGroupRequired:     "Indiquez un nom de groupe.",
		uiKeyGroupNotFound:     "Le groupe de traduction `%[1]s` est introuvable sur ce serveur.",
		uiKeyJoinGroupNotFound: "Le groupe de traduction `%[1]s` est introuvable sur ce serveur. Vérifiez qu'il correspond à un groupe créé avec `/new-channel`.",
		uiKeyDuplicateGroup:    "Le groupe de traduction `%[1]s` existe déjà sur ce serveur.",
		uiKeyDuplicateChannel:  "Ce salon a déjà rejoint le groupe `%[1]s`.",
		uiKeyDuplicateLanguage: "Le groupe `%[1]s` possède déjà un salon dans la même langue. Indiquez une autre langue.",
		uiKeyChannelNotJoined:  "Ce salon n'a pas rejoint le groupe `%[1]s`.",
		uiKeyChannelRegistered: "<#%[2]s> (%[3]s) a été enregistré dans le groupe de traduction `%[1]s`.",
		uiKeyChannelJoined:     "<#%[2]s> (%[3]s) a rejoint le groupe de traduction `%[1]s`.",
		uiKeyChannelLeft:       "<#%[2]s> a été retiré du groupe de traduction `%[1]s`.",
		uiKeyGroupDeleted:      "Le groupe de traduction `%[1]s` a été supprimé.",

		uiKeyNoGroups:        "Aucun groupe de traduction n'est enregistré sur ce serveur.",
		uiKeyGroupsHeader:    "Groupes de traduction (%[1]d) :",
		uiKeyGroupsTruncated: "\n(groupes restants omis)",
		uiKeyGroupNoChannels: "  (aucun salon)",

		uiKeyGlossaryTermRequired:   "Indiquez à la fois un terme et une traduction.",
		uiKeyGlossaryFull:           "Le glossaire de ce serveur est plein (%[1]d entrées maximum).",
		uiKeyGlossaryAdded:          "Le terme `%[1]s` a été enregistré comme `%[2]s` (%[3]s, %[4]s).",
		uiKeyGlossaryAttributeNone:  "aucun attribut",
		uiKeyGlossaryAttributeLabel: "attribut : %[1]s",
		uiKeyGlossaryModeAlways:     "toujours appliqué",
		uiKeyGlossaryModeMatched:    "appliqué quand le message contient le terme",
		uiKeyGlossaryRemoved:        "Le terme `%[1]s` a été supprimé.",
		uiKeyGlossaryNotFound:       "Le terme `%[1]s` n'est pas enregistré.",
		uiKeyNoGlossary:             "Aucune entrée de glossaire n'est enregistrée sur ce serveur.",
		uiKeyGlossaryHeader:         "Glossaire (%[1]d) :",
		uiKeyGlossaryTruncated:      "\n(entrées restantes omises)",

		uiKeyStyleBothSpecified: "Vous ne pouvez pas indiquer à la fois un préréglage et une instruction personnalisée. N'en indiquez qu'un seul.",
		uiKeyStyleNoneSpecified: "Indiquez un préréglage ou une instruction personnalisée. Pour réinitialiser le style, indiquez `preset:default`.",
		uiKeyStyleUnknownPreset: "Préréglage inconnu. Choisissez parmi les options de la commande.",
		uiKeyStyleCustomEmpty:   "L'instruction personnalisée ne peut pas être vide.",
		uiKeyStyleCustomTooLong: "L'instruction personnalisée doit comporter au maximum %[1]d caractères.",
		uiKeyStyleCustomSet:     "Le style du groupe de traduction `%[1]s` a été défini sur une instruction personnalisée : `%[2]s`",
		uiKeyStylePresetSet:     "Le style du groupe de traduction `%[1]s` a été défini sur le préréglage `%[2]s`.",
		uiKeyStyleReset:         "Le style du groupe de traduction `%[1]s` a été réinitialisé.",
	},
	"de": {
		uiKeyViewOriginalLink:       "Zur Originalnachricht",
		uiKeyOriginalMessage:        "Quelle",
		uiKeyOriginalMessageDeleted: "Die ursprüngliche Nachricht wurde gelöscht",
		uiKeyAlreadyOriginal:        "Diese Nachricht ist bereits das Original.",
		uiKeyNotManaged:             "Diese Nachricht wird vom Übersetzungsbot nicht verwaltet.",
		uiKeyForwarded:              "Weitergeleitet",

		uiKeyRateLimitNotice:         "Diese Nachricht wurde nicht übersetzt, weil das Übersetzungsratenlimit erreicht wurde.",
		uiKeyTranslationFailedNotice: "Diese Nachricht wurde nicht gespiegelt, weil die Übersetzung fehlgeschlagen ist.",

		uiKeyUnexpectedError:        "Ein interner Fehler ist aufgetreten. Bitte versuche es später erneut.",
		uiKeyInvalidLanguage:        "Gib die Sprache als kurzen BCP-47-Code an, z. B. `en`, `ja`, `zh-CN` oder `pt-BR`.",
		uiKeyChannelFetchFailed:     "Der Kanal konnte nicht abgerufen werden.",
		uiKeyUnsupportedChannelType: "Es werden nur Text-, Ankündigungs-, Forum- und Medienkanäle unterstützt.",
		uiKeyWebhookCreateFailed:    "Der Webhook konnte nicht erstellt werden.",

		uiKeyGroupRequired:     "Gib einen Gruppennamen an.",
		uiKeyGroupNotFound:     "Die Übersetzungsgruppe `%[1]s` wurde auf diesem Server nicht gefunden.",
		uiKeyJoinGroupNotFound: "Die Übersetzungsgruppe `%[1]s` wurde auf diesem Server nicht gefunden. Stelle sicher, dass sie mit einer über `/new-channel` erstellten Gruppe übereinstimmt.",
		uiKeyDuplicateGroup:    "Die Übersetzungsgruppe `%[1]s` existiert bereits auf diesem Server.",
		uiKeyDuplicateChannel:  "Dieser Kanal ist der Gruppe `%[1]s` bereits beigetreten.",
		uiKeyDuplicateLanguage: "Die Gruppe `%[1]s` hat bereits einen Kanal mit derselben Sprache. Gib eine andere Sprache an.",
		uiKeyChannelNotJoined:  "Dieser Kanal ist der Gruppe `%[1]s` nicht beigetreten.",
		uiKeyChannelRegistered: "<#%[2]s> (%[3]s) wurde in der Übersetzungsgruppe `%[1]s` registriert.",
		uiKeyChannelJoined:     "<#%[2]s> (%[3]s) ist der Übersetzungsgruppe `%[1]s` beigetreten.",
		uiKeyChannelLeft:       "<#%[2]s> wurde aus der Übersetzungsgruppe `%[1]s` entfernt.",
		uiKeyGroupDeleted:      "Die Übersetzungsgruppe `%[1]s` wurde gelöscht.",

		uiKeyNoGroups:        "Auf diesem Server sind keine Übersetzungsgruppen registriert.",
		uiKeyGroupsHeader:    "Übersetzungsgruppen (%[1]d):",
		uiKeyGroupsTruncated: "\n(weitere Gruppen ausgelassen)",
		uiKeyGroupNoChannels: "  (keine Kanäle)",

		uiKeyGlossaryTermRequired:   "Gib sowohl einen Begriff als auch eine Übersetzung an.",
		uiKeyGlossaryFull:           "Das Glossar dieses Servers ist voll (maximal %[1]d Einträge).",
		uiKeyGlossaryAdded:          "Der Begriff `%[1]s` wurde als `%[2]s` registriert (%[3]s, %[4]s).",
		uiKeyGlossaryAttributeNone:  "kein Attribut",
		uiKeyGlossaryAttributeLabel: "Attribut: %[1]s",
		uiKeyGlossaryModeAlways:     "immer angewendet",
		uiKeyGlossaryModeMatched:    "angewendet, wenn die Nachricht den Begriff enthält",
		uiKeyGlossaryRemoved:        "Der Begriff `%[1]s` wurde entfernt.",
		uiKeyGlossaryNotFound:       "Der Begriff `%[1]s` ist nicht registriert.",
		uiKeyNoGlossary:             "Auf diesem Server sind keine Glossareinträge registriert.",
		uiKeyGlossaryHeader:         "Glossar (%[1]d):",
		uiKeyGlossaryTruncated:      "\n(weitere Einträge ausgelassen)",

		uiKeyStyleBothSpecified: "Du kannst nicht gleichzeitig ein Preset und eine benutzerdefinierte Anweisung angeben. Gib nur eines an.",
		uiKeyStyleNoneSpecified: "Gib entweder ein Preset oder eine benutzerdefinierte Anweisung an. Um den Stil zurückzusetzen, gib `preset:default` an.",
		uiKeyStyleUnknownPreset: "Unbekanntes Preset. Wähle eine der Befehlsoptionen.",
		uiKeyStyleCustomEmpty:   "Die benutzerdefinierte Anweisung darf nicht leer sein.",
		uiKeyStyleCustomTooLong: "Die benutzerdefinierte Anweisung darf höchstens %[1]d Zeichen lang sein.",
		uiKeyStyleCustomSet:     "Der Stil der Übersetzungsgruppe `%[1]s` wurde auf eine benutzerdefinierte Anweisung gesetzt: `%[2]s`",
		uiKeyStylePresetSet:     "Der Stil der Übersetzungsgruppe `%[1]s` wurde auf das Preset `%[2]s` gesetzt.",
		uiKeyStyleReset:         "Der Stil der Übersetzungsgruppe `%[1]s` wurde zurückgesetzt.",
	},
	"es": {
		uiKeyViewOriginalLink:       "Ir al mensaje original",
		uiKeyOriginalMessage:        "Ver fuente",
		uiKeyOriginalMessageDeleted: "El mensaje original fue eliminado",
		uiKeyAlreadyOriginal:        "Este mensaje ya es el original.",
		uiKeyNotManaged:             "Este mensaje no está gestionado por el bot de traducción.",
		uiKeyForwarded:              "Reenviado",

		uiKeyRateLimitNotice:         "Este mensaje no se tradujo porque se alcanzó el límite de traducciones.",
		uiKeyTranslationFailedNotice: "Este mensaje no se replicó porque falló la traducción.",

		uiKeyUnexpectedError:        "Se produjo un error interno. Inténtalo de nuevo más tarde.",
		uiKeyInvalidLanguage:        "Especifica el idioma con un código BCP-47 corto, como `en`, `ja`, `zh-CN` o `pt-BR`.",
		uiKeyChannelFetchFailed:     "No se pudo obtener el canal.",
		uiKeyUnsupportedChannelType: "Solo se admiten canales de texto, anuncios, foros y multimedia.",
		uiKeyWebhookCreateFailed:    "No se pudo crear el webhook.",

		uiKeyGroupRequired:     "Especifica un nombre de grupo.",
		uiKeyGroupNotFound:     "No se encontró el grupo de traducción `%[1]s` en este servidor.",
		uiKeyJoinGroupNotFound: "No se encontró el grupo de traducción `%[1]s` en este servidor. Comprueba que coincida con un grupo creado con `/new-channel`.",
		uiKeyDuplicateGroup:    "El grupo de traducción `%[1]s` ya existe en este servidor.",
		uiKeyDuplicateChannel:  "Este canal ya se unió al grupo `%[1]s`.",
		uiKeyDuplicateLanguage: "El grupo `%[1]s` ya tiene un canal con el mismo idioma. Especifica otro idioma.",
		uiKeyChannelNotJoined:  "Este canal no se ha unido al grupo `%[1]s`.",
		uiKeyChannelRegistered: "Se registró <#%[2]s> (%[3]s) en el grupo de traducción `%[1]s`.",
		uiKeyChannelJoined:     "<#%[2]s> (%[3]s) se unió al grupo de traducción `%[1]s`.",
		uiKeyChannelLeft:       "Se quitó <#%[2]s> del grupo de traducción `%[1]s`.",
		uiKeyGroupDeleted:      "Se eliminó el grupo de traducción `%[1]s`.",

		uiKeyNoGroups:        "No hay grupos de traducción registrados en este servidor.",
		uiKeyGroupsHeader:    "Grupos de traducción (%[1]d):",
		uiKeyGroupsTruncated: "\n(se omitieron los grupos restantes)",
		uiKeyGroupNoChannels: "  (sin canales)",

		uiKeyGlossaryTermRequired:   "Especifica tanto el término como la traducción.",
		uiKeyGlossaryFull:           "El glosario de este servidor está lleno (máximo %[1]d entradas).",
		uiKeyGlossaryAdded:          "Se registró el término `%[1]s` como `%[2]s` (%[3]s, %[4]s).",
		uiKeyGlossaryAttributeNone:  "sin atributo",
		uiKeyGlossaryAttributeLabel: "atributo: %[1]s",
		uiKeyGlossaryModeAlways:     "siempre aplicado",
		uiKeyGlossaryModeMatched:    "aplicado cuando el mensaje contiene el término",
		uiKeyGlossaryRemoved:        "Se eliminó el término `%[1]s`.",
		uiKeyGlossaryNotFound:       "El término `%[1]s` no está registrado.",
		uiKeyNoGlossary:             "No hay entradas de glosario registradas en este servidor.",
		uiKeyGlossaryHeader:         "Glosario (%[1]d):",
		uiKeyGlossaryTruncated:      "\n(se omitieron las entradas restantes)",

		uiKeyStyleBothSpecified: "No puedes especificar un preajuste y una instrucción personalizada a la vez. Especifica solo uno.",
		uiKeyStyleNoneSpecified: "Especifica un preajuste o una instrucción personalizada. Para restablecer el estilo, especifica `preset:default`.",
		uiKeyStyleUnknownPreset: "Preajuste desconocido. Elige una de las opciones del comando.",
		uiKeyStyleCustomEmpty:   "La instrucción personalizada no puede estar vacía.",
		uiKeyStyleCustomTooLong: "La instrucción personalizada debe tener como máximo %[1]d caracteres.",
		uiKeyStyleCustomSet:     "Se estableció el estilo del grupo de traducción `%[1]s` con una instrucción personalizada: `%[2]s`",
		uiKeyStylePresetSet:     "Se estableció el estilo del grupo de traducción `%[1]s` con el preajuste `%[2]s`.",
		uiKeyStyleReset:         "Se restableció el estilo del grupo de traducción `%[1]s`.",
	},
	"pt-BR": {
		uiKeyViewOriginalLink:       "Ir para a mensagem original",
		uiKeyOriginalMessage:        "Ver fonte",
		uiKeyOriginalMessageDeleted: "A mensagem original foi excluída",
		uiKeyAlreadyOriginal:        "Esta mensagem já é o original.",
		uiKeyNotManaged:             "Esta mensagem não é gerenciada pelo bot de tradução.",
		uiKeyForwarded:              "Encaminhado",

		uiKeyRateLimitNotice:         "Esta mensagem não foi traduzida porque o limite de traduções foi atingido.",
		uiKeyTranslationFailedNotice: "Esta mensagem não foi espelhada porque a tradução falhou.",

		uiKeyUnexpectedError:        "Ocorreu um erro interno. Tente novamente mais tarde.",
		uiKeyInvalidLanguage:        "Especifique o idioma com um código BCP-47 curto, como `en`, `ja`, `zh-CN` ou `pt-BR`.",
		uiKeyChannelFetchFailed:     "Não foi possível obter o canal.",
		uiKeyUnsupportedChannelType: "Somente canais de texto, anúncios, fórum e mídia são compatíveis.",
		uiKeyWebhookCreateFailed:    "Não foi possível criar o webhook.",

		uiKeyGroupRequired:     "Especifique um nome de grupo.",
		uiKeyGroupNotFound:     "O grupo de tradução `%[1]s` não foi encontrado neste servidor.",
		uiKeyJoinGroupNotFound: "O grupo de tradução `%[1]s` não foi encontrado neste servidor. Verifique se corresponde a um grupo criado com `/new-channel`.",
		uiKeyDuplicateGroup:    "O grupo de tradução `%[1]s` já existe neste servidor.",
		uiKeyDuplicateChannel:  "Este canal já entrou no grupo `%[1]s`.",
		uiKeyDuplicateLanguage: "O grupo `%[1]s` já tem um canal com o mesmo idioma. Especifique outro idioma.",
		uiKeyChannelNotJoined:  "Este canal não entrou no grupo `%[1]s`.",
		uiKeyChannelRegistered: "<#%[2]s> (%[3]s) foi registrado no grupo de tradução `%[1]s`.",
		uiKeyChannelJoined:     "<#%[2]s> (%[3]s) entrou no grupo de tradução `%[1]s`.",
		uiKeyChannelLeft:       "<#%[2]s> foi removido do grupo de tradução `%[1]s`.",
		uiKeyGroupDeleted:      "O grupo de tradução `%[1]s` foi excluído.",

		uiKeyNoGroups:        "Nenhum grupo de tradução está registrado neste servidor.",
		uiKeyGroupsHeader:    "Grupos de tradução (%[1]d):",
		uiKeyGroupsTruncated: "\n(grupos restantes omitidos)",
		uiKeyGroupNoChannels: "  (sem canais)",

		uiKeyGlossaryTermRequired:   "Especifique o termo e a tradução.",
		uiKeyGlossaryFull:           "O glossário deste servidor está cheio (máximo de %[1]d entradas).",
		uiKeyGlossaryAdded:          "O termo `%[1]s` foi registrado como `%[2]s` (%[3]s, %[4]s).",
		uiKeyGlossaryAttributeNone:  "sem atributo",
		uiKeyGlossaryAttributeLabel: "atributo: %[1]s",
		uiKeyGlossaryModeAlways:     "sempre aplicado",
		uiKeyGlossaryModeMatched:    "aplicado quando a mensagem contém o termo",
		uiKeyGlossaryRemoved:        "O termo `%[1]s` foi removido.",
		uiKeyGlossaryNotFound:       "O termo `%[1]s` não está registrado.",
		uiKeyNoGlossary:             "Nenhuma entrada de glossário está registrada neste servidor.",
		uiKeyGlossaryHeader:         "Glossário (%[1]d):",
		uiKeyGlossaryTruncated:      "\n(entradas restantes omitidas)",

		uiKeyStyleBothSpecified: "Você não pode especificar um preset e uma instrução personalizada ao mesmo tempo. Especifique apenas um.",
		uiKeyStyleNoneSpecified: "Especifique um preset ou uma instrução personalizada. Para redefinir o estilo, especifique `preset:default`.",
		uiKeyStyleUnknownPreset: "Preset desconhecido. Escolha uma das opções do comando.",
		uiKeyStyleCustomEmpty:   "A instrução personalizada não pode estar vazia.",
		uiKeyStyleCustomTooLong: "A instrução personalizada deve ter no máximo %[1]d caracteres.",
		uiKeyStyleCustomSet:     "O estilo do grupo de tradução `%[1]s` foi definido com uma instrução personalizada: `%[2]s`",
		uiKeyStylePresetSet:     "O estilo do grupo de tradução `%[1]s` foi definido com o preset `%[2]s`.",
		uiKeyStyleReset:         "O estilo do grupo de tradução `%[1]s` foi redefinido.",
	},
	"it": {
		uiKeyViewOriginalLink:       "Vai al messaggio originale",
		uiKeyOriginalMessage:        "Fonte",
		uiKeyOriginalMessageDeleted: "Il messaggio originale è stato eliminato",
		uiKeyAlreadyOriginal:        "Questo messaggio è già l'originale.",
		uiKeyNotManaged:             "Questo messaggio non è gestito dal bot di traduzione.",
		uiKeyForwarded:              "Inoltrato",

		uiKeyRateLimitNotice:         "Questo messaggio non è stato tradotto perché è stato raggiunto il limite di traduzioni.",
		uiKeyTranslationFailedNotice: "Questo messaggio non è stato replicato perché la traduzione non è riuscita.",

		uiKeyUnexpectedError:        "Si è verificato un errore interno. Riprova più tardi.",
		uiKeyInvalidLanguage:        "Specifica la lingua con un codice BCP-47 breve, ad esempio `en`, `ja`, `zh-CN` o `pt-BR`.",
		uiKeyChannelFetchFailed:     "Impossibile recuperare il canale.",
		uiKeyUnsupportedChannelType: "Sono supportati solo canali di testo, annunci, forum e multimediali.",
		uiKeyWebhookCreateFailed:    "Impossibile creare il webhook.",

		uiKeyGroupRequired:     "Specifica un nome di gruppo.",
		uiKeyGroupNotFound:     "Il gruppo di traduzione `%[1]s` non è stato trovato in questo server.",
		uiKeyJoinGroupNotFound: "Il gruppo di traduzione `%[1]s` non è stato trovato in questo server. Verifica che corrisponda a un gruppo creato con `/new-channel`.",
		uiKeyDuplicateGroup:    "Il gruppo di traduzione `%[1]s` esiste già in questo server.",
		uiKeyDuplicateChannel:  "Questo canale è già entrato nel gruppo `%[1]s`.",
		uiKeyDuplicateLanguage: "Il gruppo `%[1]s` ha già un canale con la stessa lingua. Specifica un'altra lingua.",
		uiKeyChannelNotJoined:  "Questo canale non è entrato nel gruppo `%[1]s`.",
		uiKeyChannelRegistered: "<#%[2]s> (%[3]s) è stato registrato nel gruppo di traduzione `%[1]s`.",
		uiKeyChannelJoined:     "<#%[2]s> (%[3]s) è entrato nel gruppo di traduzione `%[1]s`.",
		uiKeyChannelLeft:       "<#%[2]s> è stato rimosso dal gruppo di traduzione `%[1]s`.",
		uiKeyGroupDeleted:      "Il gruppo di traduzione `%[1]s` è stato eliminato.",

		uiKeyNoGroups:        "Nessun gruppo di traduzione è registrato in questo server.",
		uiKeyGroupsHeader:    "Gruppi di traduzione (%[1]d):",
		uiKeyGroupsTruncated: "\n(gruppi rimanenti omessi)",
		uiKeyGroupNoChannels: "  (nessun canale)",

		uiKeyGlossaryTermRequired:   "Specifica sia il termine sia la traduzione.",
		uiKeyGlossaryFull:           "Il glossario di questo server è pieno (massimo %[1]d voci).",
		uiKeyGlossaryAdded:          "Il termine `%[1]s` è stato registrato come `%[2]s` (%[3]s, %[4]s).",
		uiKeyGlossaryAttributeNone:  "nessun attributo",
		uiKeyGlossaryAttributeLabel: "attributo: %[1]s",
		uiKeyGlossaryModeAlways:     "sempre applicato",
		uiKeyGlossaryModeMatched:    "applicato quando il messaggio contiene il termine",
		uiKeyGlossaryRemoved:        "Il termine `%[1]s` è stato rimosso.",
		uiKeyGlossaryNotFound:       "Il termine `%[1]s` non è registrato.",
		uiKeyNoGlossary:             "Nessuna voce di glossario è registrata in questo server.",
		uiKeyGlossaryHeader:         "Glossario (%[1]d):",
		uiKeyGlossaryTruncated:      "\n(voci rimanenti omesse)",

		uiKeyStyleBothSpecified: "Non puoi specificare sia un preset sia un'istruzione personalizzata. Specificane solo uno.",
		uiKeyStyleNoneSpecified: "Specifica un preset o un'istruzione personalizzata. Per reimpostare lo stile, specifica `preset:default`.",
		uiKeyStyleUnknownPreset: "Preset sconosciuto. Scegli una delle opzioni del comando.",
		uiKeyStyleCustomEmpty:   "L'istruzione personalizzata non può essere vuota.",
		uiKeyStyleCustomTooLong: "L'istruzione personalizzata deve contenere al massimo %[1]d caratteri.",
		uiKeyStyleCustomSet:     "Lo stile del gruppo di traduzione `%[1]s` è stato impostato su un'istruzione personalizzata: `%[2]s`",
		uiKeyStylePresetSet:     "Lo stile del gruppo di traduzione `%[1]s` è stato impostato sul preset `%[2]s`.",
		uiKeyStyleReset:         "Lo stile del gruppo di traduzione `%[1]s` è stato reimpostato.",
	},
	"id": {
		uiKeyViewOriginalLink:       "Buka pesan asli",
		uiKeyOriginalMessage:        "Lihat sumber",
		uiKeyOriginalMessageDeleted: "Pesan asli telah dihapus",
		uiKeyAlreadyOriginal:        "Pesan ini sudah merupakan pesan asli.",
		uiKeyNotManaged:             "Pesan ini tidak dikelola oleh bot terjemahan.",
		uiKeyForwarded:              "Diteruskan",

		uiKeyRateLimitNotice:         "Pesan ini tidak diterjemahkan karena batas laju terjemahan telah tercapai.",
		uiKeyTranslationFailedNotice: "Pesan ini tidak dicerminkan karena terjemahan gagal.",

		uiKeyUnexpectedError:        "Terjadi kesalahan internal. Silakan coba lagi nanti.",
		uiKeyInvalidLanguage:        "Tentukan bahasa dengan kode BCP-47 pendek, misalnya `en`, `ja`, `zh-CN`, atau `pt-BR`.",
		uiKeyChannelFetchFailed:     "Tidak dapat mengambil channel.",
		uiKeyUnsupportedChannelType: "Hanya channel teks, pengumuman, forum, dan media yang didukung.",
		uiKeyWebhookCreateFailed:    "Tidak dapat membuat webhook.",

		uiKeyGroupRequired:     "Tentukan nama grup.",
		uiKeyGroupNotFound:     "Grup terjemahan `%[1]s` tidak ditemukan di server ini.",
		uiKeyJoinGroupNotFound: "Grup terjemahan `%[1]s` tidak ditemukan di server ini. Pastikan namanya cocok dengan grup yang dibuat dengan `/new-channel`.",
		uiKeyDuplicateGroup:    "Grup terjemahan `%[1]s` sudah ada di server ini.",
		uiKeyDuplicateChannel:  "Channel ini sudah bergabung dengan grup `%[1]s`.",
		uiKeyDuplicateLanguage: "Grup `%[1]s` sudah memiliki channel dengan bahasa yang sama. Tentukan bahasa lain.",
		uiKeyChannelNotJoined:  "Channel ini belum bergabung dengan grup `%[1]s`.",
		uiKeyChannelRegistered: "<#%[2]s> (%[3]s) telah didaftarkan ke grup terjemahan `%[1]s`.",
		uiKeyChannelJoined:     "<#%[2]s> (%[3]s) telah bergabung dengan grup terjemahan `%[1]s`.",
		uiKeyChannelLeft:       "<#%[2]s> telah dikeluarkan dari grup terjemahan `%[1]s`.",
		uiKeyGroupDeleted:      "Grup terjemahan `%[1]s` telah dihapus.",

		uiKeyNoGroups:        "Tidak ada grup terjemahan yang terdaftar di server ini.",
		uiKeyGroupsHeader:    "Grup terjemahan (%[1]d):",
		uiKeyGroupsTruncated: "\n(grup lainnya dihilangkan)",
		uiKeyGroupNoChannels: "  (tidak ada channel)",

		uiKeyGlossaryTermRequired:   "Tentukan istilah dan terjemahannya.",
		uiKeyGlossaryFull:           "Glosarium server ini sudah penuh (maksimum %[1]d entri).",
		uiKeyGlossaryAdded:          "Istilah `%[1]s` telah didaftarkan sebagai `%[2]s` (%[3]s, %[4]s).",
		uiKeyGlossaryAttributeNone:  "tanpa atribut",
		uiKeyGlossaryAttributeLabel: "atribut: %[1]s",
		uiKeyGlossaryModeAlways:     "selalu diterapkan",
		uiKeyGlossaryModeMatched:    "diterapkan saat pesan mengandung istilah tersebut",
		uiKeyGlossaryRemoved:        "Istilah `%[1]s` telah dihapus.",
		uiKeyGlossaryNotFound:       "Istilah `%[1]s` tidak terdaftar.",
		uiKeyNoGlossary:             "Tidak ada entri glosarium yang terdaftar di server ini.",
		uiKeyGlossaryHeader:         "Glosarium (%[1]d):",
		uiKeyGlossaryTruncated:      "\n(entri lainnya dihilangkan)",

		uiKeyStyleBothSpecified: "Anda tidak dapat menentukan preset dan instruksi kustom sekaligus. Tentukan salah satu saja.",
		uiKeyStyleNoneSpecified: "Tentukan preset atau instruksi kustom. Untuk mengatur ulang gaya, tentukan `preset:default`.",
		uiKeyStyleUnknownPreset: "Preset tidak dikenal. Pilih salah satu opsi perintah.",
		uiKeyStyleCustomEmpty:   "Instruksi kustom tidak boleh kosong.",
		uiKeyStyleCustomTooLong: "Instruksi kustom maksimum %[1]d karakter.",
		uiKeyStyleCustomSet:     "Gaya grup terjemahan `%[1]s` telah diatur ke instruksi kustom: `%[2]s`",
		uiKeyStylePresetSet:     "Gaya grup terjemahan `%[1]s` telah diatur ke preset `%[2]s`.",
		uiKeyStyleReset:         "Gaya grup terjemahan `%[1]s` telah diatur ulang.",
	},
	"th": {
		uiKeyViewOriginalLink:       "ไปยังข้อความต้นฉบับ",
		uiKeyOriginalMessage:        "ดูต้นฉบับ",
		uiKeyOriginalMessageDeleted: "ข้อความต้นฉบับถูกลบแล้ว",
		uiKeyAlreadyOriginal:        "ข้อความนี้เป็นข้อความต้นฉบับอยู่แล้ว",
		uiKeyNotManaged:             "ข้อความนี้ไม่ได้อยู่ภายใต้การจัดการของบอทแปลภาษา",
		uiKeyForwarded:              "ส่งต่อแล้ว",

		uiKeyRateLimitNotice:         "ข้อความนี้ไม่ได้รับการแปลเนื่องจากถึงขีดจำกัดอัตราการแปลแล้ว",
		uiKeyTranslationFailedNotice: "ข้อความนี้ไม่ได้ถูกมิเรอร์เนื่องจากการแปลล้มเหลว",

		uiKeyUnexpectedError:        "เกิดข้อผิดพลาดภายใน โปรดลองอีกครั้งในภายหลัง",
		uiKeyInvalidLanguage:        "โปรดระบุภาษาด้วยรหัส BCP-47 แบบสั้น เช่น `en`, `ja`, `zh-CN` หรือ `pt-BR`",
		uiKeyChannelFetchFailed:     "ไม่สามารถดึงข้อมูลช่องได้",
		uiKeyUnsupportedChannelType: "รองรับเฉพาะช่องข้อความ ประกาศ ฟอรัม และมีเดียเท่านั้น",
		uiKeyWebhookCreateFailed:    "ไม่สามารถสร้าง webhook ได้",

		uiKeyGroupRequired:     "โปรดระบุชื่อกลุ่ม",
		uiKeyGroupNotFound:     "ไม่พบกลุ่มการแปล `%[1]s` ในเซิร์ฟเวอร์นี้",
		uiKeyJoinGroupNotFound: "ไม่พบกลุ่มการแปล `%[1]s` ในเซิร์ฟเวอร์นี้ โปรดตรวจสอบว่าตรงกับชื่อกลุ่มที่สร้างด้วย `/new-channel`",
		uiKeyDuplicateGroup:    "กลุ่มการแปล `%[1]s` มีอยู่แล้วในเซิร์ฟเวอร์นี้",
		uiKeyDuplicateChannel:  "ช่องนี้เข้าร่วมกลุ่ม `%[1]s` อยู่แล้ว",
		uiKeyDuplicateLanguage: "กลุ่ม `%[1]s` มีช่องที่ใช้ภาษาเดียวกันอยู่แล้ว โปรดระบุภาษาอื่น",
		uiKeyChannelNotJoined:  "ช่องนี้ยังไม่ได้เข้าร่วมกลุ่ม `%[1]s`",
		uiKeyChannelRegistered: "ลงทะเบียน <#%[2]s> (%[3]s) ในกลุ่มการแปล `%[1]s` แล้ว",
		uiKeyChannelJoined:     "เพิ่ม <#%[2]s> (%[3]s) เข้าร่วมกลุ่มการแปล `%[1]s` แล้ว",
		uiKeyChannelLeft:       "นำ <#%[2]s> ออกจากกลุ่มการแปล `%[1]s` แล้ว",
		uiKeyGroupDeleted:      "ลบกลุ่มการแปล `%[1]s` แล้ว",

		uiKeyNoGroups:        "ไม่มีกลุ่มการแปลที่ลงทะเบียนในเซิร์ฟเวอร์นี้",
		uiKeyGroupsHeader:    "กลุ่มการแปล (%[1]d):",
		uiKeyGroupsTruncated: "\n(ละกลุ่มที่เหลือ)",
		uiKeyGroupNoChannels: "  (ไม่มีช่อง)",

		uiKeyGlossaryTermRequired:   "โปรดระบุทั้งคำศัพท์และคำแปล",
		uiKeyGlossaryFull:           "อภิธานศัพท์ของเซิร์ฟเวอร์นี้เต็มแล้ว (สูงสุด %[1]d รายการ)",
		uiKeyGlossaryAdded:          "ลงทะเบียนคำศัพท์ `%[1]s` เป็น `%[2]s` แล้ว (%[3]s, %[4]s)",
		uiKeyGlossaryAttributeNone:  "ไม่มีแอตทริบิวต์",
		uiKeyGlossaryAttributeLabel: "แอตทริบิวต์: %[1]s",
		uiKeyGlossaryModeAlways:     "ใช้เสมอ",
		uiKeyGlossaryModeMatched:    "ใช้เมื่อข้อความมีคำศัพท์นั้น",
		uiKeyGlossaryRemoved:        "ลบคำศัพท์ `%[1]s` แล้ว",
		uiKeyGlossaryNotFound:       "คำศัพท์ `%[1]s` ยังไม่ได้ลงทะเบียน",
		uiKeyNoGlossary:             "ไม่มีรายการอภิธานศัพท์ที่ลงทะเบียนในเซิร์ฟเวอร์นี้",
		uiKeyGlossaryHeader:         "อภิธานศัพท์ (%[1]d):",
		uiKeyGlossaryTruncated:      "\n(ละรายการที่เหลือ)",

		uiKeyStyleBothSpecified: "ไม่สามารถระบุพรีเซ็ตและคำสั่งกำหนดเองพร้อมกันได้ โปรดระบุเพียงอย่างใดอย่างหนึ่ง",
		uiKeyStyleNoneSpecified: "โปรดระบุพรีเซ็ตหรือคำสั่งกำหนดเอง หากต้องการรีเซ็ตสไตล์ ให้ระบุ `preset:default`",
		uiKeyStyleUnknownPreset: "ไม่รู้จักพรีเซ็ตนี้ โปรดเลือกจากตัวเลือกของคำสั่ง",
		uiKeyStyleCustomEmpty:   "คำสั่งกำหนดเองต้องไม่ว่างเปล่า",
		uiKeyStyleCustomTooLong: "คำสั่งกำหนดเองต้องมีความยาวไม่เกิน %[1]d อักขระ",
		uiKeyStyleCustomSet:     "ตั้งค่าสไตล์ของกลุ่มการแปล `%[1]s` เป็นคำสั่งกำหนดเองแล้ว: `%[2]s`",
		uiKeyStylePresetSet:     "ตั้งค่าสไตล์ของกลุ่มการแปล `%[1]s` เป็นพรีเซ็ต `%[2]s` แล้ว",
		uiKeyStyleReset:         "รีเซ็ตสไตล์ของกลุ่มการแปล `%[1]s` แล้ว",
	},
	"vi": {
		uiKeyViewOriginalLink:       "Đi tới tin nhắn gốc",
		uiKeyOriginalMessage:        "Xem nguồn",
		uiKeyOriginalMessageDeleted: "Tin nhắn gốc đã bị xóa",
		uiKeyAlreadyOriginal:        "Tin nhắn này đã là bản gốc.",
		uiKeyNotManaged:             "Tin nhắn này không được bot dịch quản lý.",
		uiKeyForwarded:              "Đã chuyển tiếp",

		uiKeyRateLimitNotice:         "Tin nhắn này không được dịch vì đã đạt giới hạn tốc độ dịch.",
		uiKeyTranslationFailedNotice: "Tin nhắn này không được sao chép vì dịch thất bại.",

		uiKeyUnexpectedError:        "Đã xảy ra lỗi nội bộ. Vui lòng thử lại sau.",
		uiKeyInvalidLanguage:        "Hãy chỉ định ngôn ngữ bằng mã BCP-47 ngắn, ví dụ `en`, `ja`, `zh-CN` hoặc `pt-BR`.",
		uiKeyChannelFetchFailed:     "Không thể lấy thông tin kênh.",
		uiKeyUnsupportedChannelType: "Chỉ hỗ trợ kênh văn bản, thông báo, diễn đàn và phương tiện.",
		uiKeyWebhookCreateFailed:    "Không thể tạo webhook.",

		uiKeyGroupRequired:     "Hãy chỉ định tên nhóm.",
		uiKeyGroupNotFound:     "Không tìm thấy nhóm dịch `%[1]s` trong máy chủ này.",
		uiKeyJoinGroupNotFound: "Không tìm thấy nhóm dịch `%[1]s` trong máy chủ này. Hãy kiểm tra xem tên có khớp với nhóm được tạo bằng `/new-channel` không.",
		uiKeyDuplicateGroup:    "Nhóm dịch `%[1]s` đã tồn tại trong máy chủ này.",
		uiKeyDuplicateChannel:  "Kênh này đã tham gia nhóm `%[1]s`.",
		uiKeyDuplicateLanguage: "Nhóm `%[1]s` đã có kênh cùng ngôn ngữ. Hãy chỉ định ngôn ngữ khác.",
		uiKeyChannelNotJoined:  "Kênh này chưa tham gia nhóm `%[1]s`.",
		uiKeyChannelRegistered: "Đã đăng ký <#%[2]s> (%[3]s) vào nhóm dịch `%[1]s`.",
		uiKeyChannelJoined:     "Đã thêm <#%[2]s> (%[3]s) vào nhóm dịch `%[1]s`.",
		uiKeyChannelLeft:       "Đã xóa <#%[2]s> khỏi nhóm dịch `%[1]s`.",
		uiKeyGroupDeleted:      "Đã xóa nhóm dịch `%[1]s`.",

		uiKeyNoGroups:        "Máy chủ này chưa đăng ký nhóm dịch nào.",
		uiKeyGroupsHeader:    "Nhóm dịch (%[1]d):",
		uiKeyGroupsTruncated: "\n(đã lược bỏ các nhóm còn lại)",
		uiKeyGroupNoChannels: "  (không có kênh)",

		uiKeyGlossaryTermRequired:   "Hãy chỉ định cả thuật ngữ và bản dịch.",
		uiKeyGlossaryFull:           "Bảng thuật ngữ của máy chủ này đã đầy (tối đa %[1]d mục).",
		uiKeyGlossaryAdded:          "Đã đăng ký thuật ngữ `%[1]s` là `%[2]s` (%[3]s, %[4]s).",
		uiKeyGlossaryAttributeNone:  "không có thuộc tính",
		uiKeyGlossaryAttributeLabel: "thuộc tính: %[1]s",
		uiKeyGlossaryModeAlways:     "luôn áp dụng",
		uiKeyGlossaryModeMatched:    "áp dụng khi tin nhắn chứa thuật ngữ",
		uiKeyGlossaryRemoved:        "Đã xóa thuật ngữ `%[1]s`.",
		uiKeyGlossaryNotFound:       "Thuật ngữ `%[1]s` chưa được đăng ký.",
		uiKeyNoGlossary:             "Máy chủ này chưa đăng ký mục thuật ngữ nào.",
		uiKeyGlossaryHeader:         "Bảng thuật ngữ (%[1]d):",
		uiKeyGlossaryTruncated:      "\n(đã lược bỏ các mục còn lại)",

		uiKeyStyleBothSpecified: "Không thể chỉ định đồng thời preset và hướng dẫn tùy chỉnh. Hãy chỉ định một trong hai.",
		uiKeyStyleNoneSpecified: "Hãy chỉ định preset hoặc hướng dẫn tùy chỉnh. Để đặt lại kiểu, hãy chỉ định `preset:default`.",
		uiKeyStyleUnknownPreset: "Preset không xác định. Hãy chọn từ các tùy chọn của lệnh.",
		uiKeyStyleCustomEmpty:   "Hướng dẫn tùy chỉnh không được để trống.",
		uiKeyStyleCustomTooLong: "Hướng dẫn tùy chỉnh tối đa %[1]d ký tự.",
		uiKeyStyleCustomSet:     "Đã đặt kiểu của nhóm dịch `%[1]s` thành hướng dẫn tùy chỉnh: `%[2]s`",
		uiKeyStylePresetSet:     "Đã đặt kiểu của nhóm dịch `%[1]s` thành preset `%[2]s`.",
		uiKeyStyleReset:         "Đã đặt lại kiểu của nhóm dịch `%[1]s`.",
	},
}

// resolveUILanguage maps any BCP-47 code (including Discord locales such as
// "en-US") to a supported catalog language, falling back to English.
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

func localizedUIString(language string, key uiKey) string {
	lang := resolveUILanguage(language)
	if table, ok := uiStrings[lang]; ok {
		if s, ok := table[key]; ok {
			return s
		}
	}
	return uiStrings["en"][key]
}

func localizedUIStringf(language string, key uiKey, args ...any) string {
	return fmt.Sprintf(localizedUIString(language, key), args...)
}
