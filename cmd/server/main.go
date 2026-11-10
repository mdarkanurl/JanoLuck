package main

import (
	"github.com/mdarkanurl/JanoLuck/internal/server"
)

func main() {
	srv := server.New()
	defer func() {
		_ = srv.Close()
	}()

	if err := srv.Start(); err != nil {
		panic(err)
	}
}
