package llm

import (
	"fmt"

	"github.com/Vadich007/meetnotes/internal/config"
	"github.com/Vadich007/meetnotes/internal/domain"
	"github.com/Vadich007/meetnotes/internal/service"
)

// New выбирает реализацию LLM-клиента по конфигурации.
func New(cfg config.LLM) (service.LLMClient, error) {
	switch cfg.Provider {
	case config.ProviderMock:
		return NewMock(MockOptions{Delay: cfg.MockDelay}), nil

	case config.ProviderGigaChat:
		return nil, fmt.Errorf("%w: провайдер %q требует учётных данных внешнего API",
			domain.ErrProviderNotConfigured, cfg.Provider)

	default:
		return nil, fmt.Errorf("%w: неизвестный LLM-провайдер %q",
			domain.ErrProviderNotConfigured, cfg.Provider)
	}
}
