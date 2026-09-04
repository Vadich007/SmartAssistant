package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/Vadich007/meetnotes/internal/domain"
	"github.com/Vadich007/meetnotes/internal/service"
)

func (r *root) startCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Зарегистрировать пользователя",
		Long:  "Создаёт пользователя, если он ещё не заведён. Остальные команды делают это автоматически.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := r.requireUser(); err != nil {
				return err
			}
			return r.withService(cmd, func(ctx context.Context, svc *service.Service) error {
				user, created, err := svc.EnsureUser(ctx, r.userID)
				if err != nil {
					return err
				}
				return r.printUser(user, created)
			})
		},
	}
}

func (r *root) loadCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "load <path>",
		Short: "Загрузить файл встречи на обработку",
		Long: "Копирует файл в хранилище, создаёт встречу и задачу обработки и сразу возвращает управление.\n" +
			"Обработку выполняет фоновый процесс (meetnotes worker); следить за ней можно командой status.\n" +
			"Поддерживаемые форматы: " + strings.Join(domain.SupportedFormats(), ", "),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := r.requireUser(); err != nil {
				return err
			}
			return r.withService(cmd, func(ctx context.Context, svc *service.Service) error {
				meeting, job, err := svc.Load(ctx, r.userID, args[0])
				if err != nil {
					return err
				}
				return r.printLoaded(meeting, job)
			})
		},
	}
}

func (r *root) listCmd() *cobra.Command {
	var limit, offset int

	cmd := &cobra.Command{
		Use:   "list",
		Short: "Показать список своих встреч",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := r.requireUser(); err != nil {
				return err
			}
			return r.withService(cmd, func(ctx context.Context, svc *service.Service) error {
				items, err := svc.List(ctx, r.userID, limit, offset)
				if err != nil {
					return err
				}
				return r.printList(items)
			})
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "сколько встреч показать")
	cmd.Flags().IntVar(&offset, "offset", 0, "сколько встреч пропустить")
	return cmd
}

func (r *root) statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status <id>",
		Short: "Показать статус обработки встречи",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := r.requireUser(); err != nil {
				return err
			}
			id, err := domain.ParseMeetingID(args[0])
			if err != nil {
				return err
			}
			return r.withService(cmd, func(ctx context.Context, svc *service.Service) error {
				view, err := svc.Status(ctx, r.userID, id)
				if err != nil {
					return err
				}
				return r.printStatus(view)
			})
		},
	}
}

func (r *root) getCmd() *cobra.Command {
	var outFile string

	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Получить транскрипцию и выжимку встречи",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := r.requireUser(); err != nil {
				return err
			}
			id, err := domain.ParseMeetingID(args[0])
			if err != nil {
				return err
			}
			return r.withService(cmd, func(ctx context.Context, svc *service.Service) error {
				details, err := svc.Get(ctx, r.userID, id)
				if err != nil {
					return err
				}
				if outFile != "" {
					return r.exportTranscript(details, outFile)
				}
				return r.printDetails(details)
			})
		},
	}
	cmd.Flags().StringVarP(&outFile, "out", "o", "", "сохранить транскрипцию в файл")
	return cmd
}

func (r *root) findCmd() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "find <keyword>",
		Short: "Найти свои встречи по ключевому слову",
		Long:  "Полнотекстовый поиск по транскрипциям и выжимкам. Ищет только среди встреч текущего пользователя.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := r.requireUser(); err != nil {
				return err
			}
			keyword := strings.Join(args, " ")
			return r.withService(cmd, func(ctx context.Context, svc *service.Service) error {
				results, err := svc.Find(ctx, r.userID, keyword, limit)
				if err != nil {
					return err
				}
				return r.printSearch(keyword, results)
			})
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 10, "сколько результатов показать")
	return cmd
}

func (r *root) chatCmd() *cobra.Command {
	var meetingRaw string

	cmd := &cobra.Command{
		Use:   "chat <text>",
		Short: "Задать вопрос по материалам встреч",
		Long: "Отправляет вопрос LLM-клиенту вместе с сохранёнными материалами.\n" +
			"С флагом --meeting контекстом служит указанная встреча, без него система\n" +
			"сама подбирает наиболее подходящие встречи пользователя.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := r.requireUser(); err != nil {
				return err
			}

			var meetingID *uuid.UUID
			if meetingRaw != "" {
				id, err := domain.ParseMeetingID(meetingRaw)
				if err != nil {
					return err
				}
				meetingID = &id
			}

			question := strings.Join(args, " ")
			return r.withService(cmd, func(ctx context.Context, svc *service.Service) error {
				answer, err := svc.Chat(ctx, r.userID, meetingID, question)
				if err != nil {
					return err
				}
				return r.printAnswer(answer)
			})
		},
	}
	cmd.Flags().StringVarP(&meetingRaw, "meeting", "m", "", "идентификатор встречи для контекста")
	return cmd
}

func (r *root) retryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "retry <id>",
		Short: "Повторить обработку встречи, завершившейся ошибкой",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := r.requireUser(); err != nil {
				return err
			}
			id, err := domain.ParseMeetingID(args[0])
			if err != nil {
				return err
			}
			return r.withService(cmd, func(ctx context.Context, svc *service.Service) error {
				job, err := svc.Retry(ctx, r.userID, id)
				if err != nil {
					return err
				}
				return r.printRetried(id, job)
			})
		},
	}
}

func (r *root) deleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>",
		Short: "Удалить встречу и все связанные с ней данные",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := r.requireUser(); err != nil {
				return err
			}
			id, err := domain.ParseMeetingID(args[0])
			if err != nil {
				return err
			}
			return r.withService(cmd, func(ctx context.Context, svc *service.Service) error {
				if err := svc.Delete(ctx, r.userID, id); err != nil {
					return err
				}
				return r.printDeleted(id)
			})
		},
	}
}

func (r *root) statsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Показать сводку по обработке встреч",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := r.requireUser(); err != nil {
				return err
			}
			return r.withService(cmd, func(ctx context.Context, svc *service.Service) error {
				stats, err := svc.Stats(ctx, r.userID)
				if err != nil {
					return err
				}
				return r.printStats(stats)
			})
		},
	}
}

func (r *root) historyCmd() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "history",
		Short: "Показать историю вопросов и ответов",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := r.requireUser(); err != nil {
				return err
			}
			return r.withService(cmd, func(ctx context.Context, svc *service.Service) error {
				entries, err := svc.History(ctx, r.userID, limit)
				if err != nil {
					return err
				}
				return r.printHistory(entries)
			})
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 10, "сколько записей показать")
	return cmd
}

// exportTranscript сохраняет расшифровку встречи в файл.
func (r *root) exportTranscript(details domain.MeetingDetails, path string) error {
	if details.Transcript == nil {
		return fmt.Errorf("%w: транскрипция встречи ещё не готова, текущий статус: %s",
			domain.ErrConflict, details.Job.Status)
	}
	if err := os.WriteFile(path, []byte(details.Transcript.Text), 0o644); err != nil {
		return fmt.Errorf("сохранение транскрипции в %s: %w", path, err)
	}
	_, err := fmt.Fprintf(r.out, "Транскрипция сохранена в %s\n", path)
	return err
}
