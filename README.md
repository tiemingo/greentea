# greentea
A Wrapper for the bubbletea golang tui libary


# Example
``` golang
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/tiemingo/greentea"
	"github.com/urfave/cli/v3"
)

func main() {

	logLeaf := greentea.NewStringLeaf()
	quitLeaf := greentea.NewLeaf[error]()
	exitLeaf := greentea.NewLeaf[func()]()

	commandError := &greentea.CommandError{
		CommandError: "",
	}

	// Set exit functions
	exitLeaf.Append(func() {
		logLeaf.Println("First exit func")
	})
	exitLeaf.Append(func() {
		logLeaf.Println("Second exit func")
	})

	go func() {
		i := 0
		for {
			logLeaf.Printlnf("%d. Print", i)
			time.Sleep(time.Second)

			if i == 10 {
				// exitLeaf.Append(nil) // exit without message
				quitLeaf.Append(fmt.Errorf("I is %d", i)) // exit with message
			}
			i++
		}
	}()

	greentea.RunTui(&greentea.GreenTeaConfig{
		RefreshDelay: 100,
		Commands: []*cli.Command{
			{
				Name: "test",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					commandError.CommandError = "test for command error"
					return nil
				},
			},
		},
		LogLeaf:  logLeaf,
		QuitLeaf: quitLeaf,
		ExitLeaf: exitLeaf,
		History: &greentea.History{
			Persistent:    true,
			SavePath:      "./",
			HistoryLength: 25,
		},
		CommandError: commandError,
	})
}
```