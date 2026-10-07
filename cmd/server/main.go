package main

import (
	"github.com/mdarkanurl/JanoLuck/internal/server"
)

func main() {
	srv := server.New()

	if err := srv.Start(); err != nil {
		panic(err)
	}
}
