package main

// The tray texts, one table for both languages; tr picks the current one. To add a language: one more column here
// and one more item in langs.

var langs = []string{"en", "ru"}

var texts = map[string]map[string]string{
	"on":                 {"en": "focuskeeper: on", "ru": "focuskeeper: работает"},
	"paused":             {"en": "focuskeeper: paused", "ru": "focuskeeper: на паузе"},
	"drySuffix":          {"en": " (dry mode)", "ru": " (пробный режим)"},
	"modeSoft":           {"en": "Soft", "ru": "Мягкий"},
	"modeHover":          {"en": "On hover", "ru": "При наведении"},
	"target":             {"en": "VM window", "ru": "Окно ВМ"},
	"custom":             {"en": "custom: ", "ru": "свой: "},
	"language":           {"en": "Language", "ru": "Язык"},
	"autostart":          {"en": "Start with Windows", "ru": "Запускать вместе с Windows"},
	"autostartElsewhere": {"en": " (set for another copy - click to use this one)", "ru": " (настроен на другую копию - нажмите, чтобы взять эту)"},
	"pause":              {"en": "Pause", "ru": "Пауза"},
	"resume":             {"en": "Resume", "ru": "Включить"},
	"openLog":            {"en": "Open log", "ru": "Открыть журнал"},
	"dry":                {"en": "Dry mode", "ru": "Пробный режим"},
	"exit":               {"en": "Exit", "ru": "Выход"},
	"lastNone":           {"en": "no actions yet", "ru": "действий ещё не было"},
	"lastAt":             {"en": "last action: ", "ru": "последнее действие: "},
	// each language by its own name
	"lang.en": {"en": "English", "ru": "English"},
	"lang.ru": {"en": "Русский", "ru": "Русский"},
}

var lang = "en"

func tr(key string) string {
	if s := texts[key][lang]; s != "" {
		return s
	}
	return texts[key]["en"]
}
