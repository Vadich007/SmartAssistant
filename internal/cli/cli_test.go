package cli_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Vadich007/meetnotes/internal/cli"
	"github.com/Vadich007/meetnotes/internal/domain"
)

func run(t *testing.T, args ...string) (string, error) {
	t.Helper()

	var out, errOut bytes.Buffer
	cmd, cleanup := cli.NewCommand(&out, &errOut)
	defer cleanup()

	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return out.String(), err
}

func TestUnknownCommand(t *testing.T) {
	_, err := run(t, "конспект")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")
}

func TestMissingRequiredArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"load без пути", []string{"--user", "alice", "load"}},
		{"status без идентификатора", []string{"--user", "alice", "status"}},
		{"get без идентификатора", []string{"--user", "alice", "get"}},
		{"find без ключевого слова", []string{"--user", "alice", "find"}},
		{"chat без вопроса", []string{"--user", "alice", "chat"}},
		{"retry без идентификатора", []string{"--user", "alice", "retry"}},
		{"delete без идентификатора", []string{"--user", "alice", "delete"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := run(t, tt.args...)
			assert.Error(t, err, "команда без обязательного аргумента должна возвращать ошибку")
		})
	}
}

func TestExtraArgumentsRejected(t *testing.T) {
	_, err := run(t, "--user", "alice", "load", "a.txt", "b.txt")
	assert.Error(t, err)

	_, err = run(t, "--user", "alice", "list", "лишний-аргумент")
	assert.Error(t, err)
}

func TestUserIsRequired(t *testing.T) {
	t.Setenv("MEETNOTES_USER", "")

	_, err := run(t, "list")

	require.ErrorIs(t, err, domain.ErrInvalidArgument)
	assert.Contains(t, err.Error(), "--user")
}

func TestInvalidMeetingID(t *testing.T) {
	t.Setenv("MEETNOTES_USER", "alice")

	for _, command := range []string{"status", "get", "retry", "delete"} {
		t.Run(command, func(t *testing.T) {
			_, err := run(t, command, "не-uuid")
			assert.ErrorIs(t, err, domain.ErrInvalidArgument)
		})
	}

	_, err := run(t, "chat", "--meeting", "не-uuid", "вопрос")
	assert.ErrorIs(t, err, domain.ErrInvalidArgument)
}

func TestInvalidConfigurationReported(t *testing.T) {
	t.Setenv("WORKER_CONCURRENCY", "-1")

	_, err := run(t, "--user", "alice", "list")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "WORKER_CONCURRENCY")
}

func TestHelpListsRequiredCommands(t *testing.T) {
	out, err := run(t, "--help")
	require.NoError(t, err)

	for _, command := range []string{"start", "load", "list", "status", "get", "find", "chat", "retry", "delete", "worker", "migrate"} {
		assert.Contains(t, out, command, "команда %s должна быть в справке", command)
	}
}
