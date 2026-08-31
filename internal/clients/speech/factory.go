package speech

import (
	"fmt"

	"github.com/Vadich007/meetnotes/internal/config"
	"github.com/Vadich007/meetnotes/internal/domain"
	"github.com/Vadich007/meetnotes/internal/service"
)

// New выбирает реализацию speech-клиента по конфигурации.
func New(cfg config.Speech) (service.SpeechClient, error) {
	switch cfg.Provider {
	case config.ProviderMock:
		return NewMock(MockOptions{Delay: cfg.MockDelay, Dir: cfg.MockDir}), nil

	case config.ProviderYandexSpeech, config.ProviderSaluteSpeech:
		return nil, fmt.Errorf("%w: провайдер %q требует учётных данных внешнего API",
			domain.ErrProviderNotConfigured, cfg.Provider)

	default:
		return nil, fmt.Errorf("%w: неизвестный speech-провайдер %q",
			domain.ErrProviderNotConfigured, cfg.Provider)
	}
}
