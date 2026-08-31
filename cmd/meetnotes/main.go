// Команда meetnotes CLI умного помощника для конспектирования встреч.
package main

import (
	"context"
	"os"

	"github.com/Vadich007/meetnotes/internal/cli"
)

func main() {
	os.Exit(cli.Execute(context.Background()))
}
