package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/DigitalN/openshuttercounter/internal/cli"
	"github.com/DigitalN/openshuttercounter/internal/cmd"
)

func main() {
	if err := cli.NewApp().Run(context.Background(), os.Args); err != nil {
		if !errors.Is(err, cmd.ErrReported) {
			fmt.Println(err.Error())
		}

		os.Exit(1)
	}
}
