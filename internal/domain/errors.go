// Package domain содержит модели предметной области и доменные ошибки.
package domain

import "errors"

var (
	// ErrNotFound запрошенная сущность не найдена. Этой же ошибкой отвечаем на
	// попытку доступа к чужой встрече.
	ErrNotFound = errors.New("не найдено")
	// ErrInvalidArgument некорректный ввод пользователя (пустой аргумент, битый UUID).
	ErrInvalidArgument = errors.New("некорректный аргумент")
	// ErrFileNotFound указанного файла нет на диске.
	ErrFileNotFound = errors.New("файл не найден")
	// ErrUnsupportedFormat расширение файла не поддерживается.
	ErrUnsupportedFormat = errors.New("формат файла не поддерживается")
	// ErrConflict операция невозможна в текущем состоянии сущности.
	ErrConflict = errors.New("операция недопустима в текущем состоянии")
	// ErrStorageUnavailable база данных временно недоступна.
	ErrStorageUnavailable = errors.New("хранилище недоступно")
	// ErrNoContext для ответа на вопрос не нашлось сохранённых материалов.
	ErrNoContext = errors.New("нет материалов для ответа")
)

// Ошибки внешних клиентов.
var (
	// ErrExternalUnavailable внешний сервис недоступен.
	ErrExternalUnavailable = errors.New("внешний сервис недоступен")
	// ErrExternalRateLimited превышен лимит запросов внешнего API.
	ErrExternalRateLimited = errors.New("превышен лимит запросов внешнего сервиса")
	// ErrProviderNotConfigured выбранный в конфигурации провайдер не настроен.
	ErrProviderNotConfigured = errors.New("провайдер не настроен")
	// ErrEmptyResult внешний сервис вернул пустой результат.
	ErrEmptyResult = errors.New("внешний сервис вернул пустой результат")
)
