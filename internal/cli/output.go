package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"

	"github.com/Vadich007/meetnotes/internal/domain"
	"github.com/Vadich007/meetnotes/internal/service"
)

const timeLayout = "02.01.2006 15:04:05"

// writeJSON печатает результат в формате json.
func (r *root) writeJSON(v any) error {
	enc := json.NewEncoder(r.out)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func (r *root) printUser(user domain.User, created bool) error {
	if r.asJSON {
		return r.writeJSON(map[string]any{
			"user_id":     user.ID,
			"external_id": user.ExternalID,
			"created":     created,
			"created_at":  user.CreatedAt,
		})
	}
	if created {
		_, err := fmt.Fprintf(r.out, "Пользователь %q зарегистрирован (id %s).\n", user.ExternalID, user.ID)
		return err
	}
	_, err := fmt.Fprintf(r.out, "С возвращением, %s! Пользователь уже зарегистрирован (id %s).\n",
		user.ExternalID, user.ID)
	return err
}

func (r *root) printLoaded(meeting domain.Meeting, job domain.Job) error {
	if r.asJSON {
		return r.writeJSON(map[string]any{
			"meeting_id": meeting.ID,
			"job_id":     job.ID,
			"title":      meeting.Title,
			"format":     meeting.Format,
			"size_bytes": meeting.SizeBytes,
			"status":     job.Status,
		})
	}
	_, err := fmt.Fprintf(r.out,
		"Встреча принята в обработку.\n"+
			"  Идентификатор: %s\n"+
			"  Название:      %s\n"+
			"  Формат:        %s (%d байт)\n"+
			"  Статус:        %s\n\n"+
			"Следить за обработкой: meetnotes status %s\n",
		meeting.ID, meeting.Title, meeting.Format, meeting.SizeBytes, job.Status, meeting.ID)
	return err
}

func (r *root) printList(items []domain.MeetingListItem) error {
	if r.asJSON {
		out := make([]map[string]any, 0, len(items))
		for _, it := range items {
			out = append(out, map[string]any{
				"meeting_id": it.Meeting.ID,
				"title":      it.Meeting.Title,
				"created_at": it.Meeting.CreatedAt,
				"status":     it.Status,
				"summary":    it.Summary,
			})
		}
		return r.writeJSON(out)
	}
	if len(items) == 0 {
		_, err := fmt.Fprintln(r.out, "Встреч пока нет. Загрузите первую: meetnotes load <path>")
		return err
	}

	tw := tabwriter.NewWriter(r.out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tСОЗДАНА\tСТАТУС\tКРАТКО")
	for _, it := range items {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			it.Meeting.ID,
			formatTime(it.Meeting.CreatedAt),
			it.Status,
			oneLine(firstMeaningfulLine(it.Summary), 60))
	}
	return tw.Flush()
}

func (r *root) printStatus(view service.StatusView) error {
	if r.asJSON {
		history := make([]map[string]any, 0, len(view.History))
		for _, h := range view.History {
			history = append(history, map[string]any{
				"from": h.From, "to": h.To, "error": h.Error, "at": h.CreatedAt,
			})
		}
		return r.writeJSON(map[string]any{
			"meeting_id":      view.Meeting.ID,
			"title":           view.Meeting.Title,
			"status":          view.Job.Status,
			"attempts":        view.Job.Attempts,
			"max_attempts":    view.Job.MaxAttempts,
			"created_at":      view.Meeting.CreatedAt,
			"status_changed":  view.Job.UpdatedAt,
			"last_error":      view.Job.LastError,
			"next_attempt_at": view.Job.NextAttemptAt,
			"history":         history,
		})
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Встреча %s: %s\n", view.Meeting.ID, view.Meeting.Title)
	fmt.Fprintf(&b, "  Статус:            %s\n", view.Job.Status)
	fmt.Fprintf(&b, "  Создана:           %s\n", formatTime(view.Meeting.CreatedAt))
	fmt.Fprintf(&b, "  Статус изменён:    %s\n", formatTime(view.Job.UpdatedAt))
	fmt.Fprintf(&b, "  Попытки:           %d из %d\n", view.Job.Attempts, view.Job.MaxAttempts)
	if view.Job.LastError != "" {
		fmt.Fprintf(&b, "  Ошибка:            %s\n", view.Job.LastError)
	}
	if len(view.History) > 0 {
		b.WriteString("\n  История статусов:\n")
		for _, h := range view.History {
			fmt.Fprintf(&b, "    %s  %s → %s", formatTime(h.CreatedAt), h.From, h.To)
			if h.Error != "" {
				fmt.Fprintf(&b, "  (%s)", oneLine(h.Error, 80))
			}
			b.WriteString("\n")
		}
	}
	_, err := fmt.Fprint(r.out, b.String())
	return err
}

func (r *root) printDetails(details domain.MeetingDetails) error {
	if r.asJSON {
		out := map[string]any{
			"meeting_id":    details.Meeting.ID,
			"title":         details.Meeting.Title,
			"original_name": details.Meeting.OriginalName,
			"created_at":    details.Meeting.CreatedAt,
			"status":        details.Job.Status,
		}
		if details.Transcript != nil {
			out["transcript"] = details.Transcript.Text
			out["language"] = details.Transcript.Language
			out["duration_sec"] = details.Transcript.DurationSec
			out["speech_provider"] = details.Transcript.Provider
		}
		if details.Summary != nil {
			out["summary"] = details.Summary.Text
			out["llm_provider"] = details.Summary.Provider
			out["llm_model"] = details.Summary.Model
		}
		return r.writeJSON(out)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Встреча %s — %s\n", details.Meeting.ID, details.Meeting.Title)
	fmt.Fprintf(&b, "  Файл:    %s\n", details.Meeting.OriginalName)
	fmt.Fprintf(&b, "  Создана: %s\n", formatTime(details.Meeting.CreatedAt))
	fmt.Fprintf(&b, "  Статус:  %s\n", details.Job.Status)

	if details.Summary != nil {
		fmt.Fprintf(&b, "\n--- Краткая выжимка (%s) ---\n%s\n",
			details.Summary.Provider, details.Summary.Text)
	}
	if details.Transcript != nil {
		fmt.Fprintf(&b, "\n--- Транскрипция (%s, %s, ~%d сек) ---\n%s\n",
			details.Transcript.Provider, details.Transcript.Language,
			details.Transcript.DurationSec, details.Transcript.Text)
	} else {
		fmt.Fprintf(&b, "\nТранскрипция ещё не готова.\n")
		if details.Job.LastError != "" {
			fmt.Fprintf(&b, "Последняя ошибка: %s\n", details.Job.LastError)
		}
	}
	_, err := fmt.Fprint(r.out, b.String())
	return err
}

func (r *root) printSearch(keyword string, results []domain.SearchResult) error {
	if r.asJSON {
		out := make([]map[string]any, 0, len(results))
		for _, res := range results {
			out = append(out, map[string]any{
				"meeting_id": res.Meeting.ID,
				"created_at": res.Meeting.CreatedAt,
				"status":     res.Status,
				"snippet":    res.Snippet,
				"rank":       res.Rank,
				"matched_in": res.MatchedIn,
			})
		}
		return r.writeJSON(out)
	}
	if len(results) == 0 {
		_, err := fmt.Fprintf(r.out, "По запросу %q ничего не найдено.\n", keyword)
		return err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Найдено встреч: %d\n\n", len(results))
	for _, res := range results {
		fmt.Fprintf(&b, "%s  %s  [%s]\n", res.Meeting.ID, formatTime(res.Meeting.CreatedAt), res.Status)
		fmt.Fprintf(&b, "  %s\n", res.Meeting.Title)
		fmt.Fprintf(&b, "  %s: %s\n\n", sourceName(res.MatchedIn), oneLine(res.Snippet, 200))
	}
	_, err := fmt.Fprint(r.out, b.String())
	return err
}

func (r *root) printAnswer(answer service.ChatAnswer) error {
	if r.asJSON {
		sources := make([]map[string]any, 0, len(answer.Sources))
		for _, m := range answer.Sources {
			sources = append(sources, map[string]any{"meeting_id": m.ID, "title": m.Title})
		}
		return r.writeJSON(map[string]any{
			"question": answer.Question,
			"answer":   answer.Answer,
			"provider": answer.Provider,
			"model":    answer.Model,
			"sources":  sources,
		})
	}

	var b strings.Builder
	b.WriteString(answer.Answer)
	b.WriteString("\n")
	if len(answer.Sources) > 0 {
		b.WriteString("\nИсточники:\n")
		for _, m := range answer.Sources {
			fmt.Fprintf(&b, "  %s — %s\n", m.ID, m.Title)
		}
	}
	_, err := fmt.Fprint(r.out, b.String())
	return err
}

func (r *root) printRetried(meetingID uuid.UUID, job domain.Job) error {
	if r.asJSON {
		return r.writeJSON(map[string]any{
			"meeting_id": meetingID,
			"job_id":     job.ID,
			"status":     job.Status,
		})
	}
	_, err := fmt.Fprintf(r.out,
		"Задача возвращена в очередь, текущий статус — %s.\nОбработку выполнит процесс meetnotes worker.\n",
		job.Status)
	return err
}

func (r *root) printDeleted(meetingID uuid.UUID) error {
	if r.asJSON {
		return r.writeJSON(map[string]any{"meeting_id": meetingID, "deleted": true})
	}
	_, err := fmt.Fprintf(r.out, "Встреча %s и все связанные данные удалены.\n", meetingID)
	return err
}

func (r *root) printStats(stats domain.Stats) error {
	if r.asJSON {
		byStatus := make(map[string]int, len(stats.ByStatus))
		for _, row := range stats.ByStatus {
			byStatus[string(row.Status)] = row.Count
		}
		return r.writeJSON(map[string]any{
			"total_meetings":     stats.TotalMeetings,
			"by_status":          byStatus,
			"avg_processing_sec": stats.AvgProcessingSec,
			"failed_jobs":        stats.FailedCount,
		})
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Всего встреч: %d\n", stats.TotalMeetings)
	fmt.Fprintf(&b, "Среднее время обработки: %.1f сек\n\n", stats.AvgProcessingSec)
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "СТАТУС\tЗАДАЧ")
	for _, row := range stats.ByStatus {
		fmt.Fprintf(tw, "%s\t%d\n", row.Status, row.Count)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	_, err := fmt.Fprint(r.out, b.String())
	return err
}

func (r *root) printHistory(entries []domain.QAEntry) error {
	if r.asJSON {
		out := make([]map[string]any, 0, len(entries))
		for _, e := range entries {
			out = append(out, map[string]any{
				"question":   e.Question,
				"answer":     e.Answer,
				"meeting_id": e.MeetingID,
				"created_at": e.CreatedAt,
			})
		}
		return r.writeJSON(out)
	}
	if len(entries) == 0 {
		_, err := fmt.Fprintln(r.out, "Вопросов пока не было.")
		return err
	}

	var b strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&b, "[%s] %s\n%s\n\n", formatTime(e.CreatedAt), e.Question, e.Answer)
	}
	_, err := fmt.Fprint(r.out, b.String())
	return err
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Local().Format(timeLayout)
}

func sourceName(matchedIn string) string {
	if matchedIn == "summary" {
		return "выжимка"
	}
	return "транскрипция"
}

// firstMeaningfulLine пропускает заголовки вроде «Кратко о встрече:» и маркеры списка.
func firstMeaningfulLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "•→-"))
		if line == "" || strings.HasSuffix(line, ":") {
			continue
		}
		return line
	}
	return strings.TrimSpace(text)
}

// oneLine схлопывает текст в одну строку и обрезает до limit символов.
func oneLine(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}
