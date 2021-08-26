package main

import (
	"context"
	"github.com/buger/goreplay/server/middleware"
	"log"
	"net/http"
	_ "net/http/pprof"
	"time"

	"github.com/gin-gonic/gin"
)

var srv = &http.Server{
	ReadTimeout:    30 * time.Second,
	WriteTimeout:   30 * time.Second,
	MaxHeaderBytes: 1 << 20,
}

// Start http server
func Start(r *gin.Engine, address string) {
	loggerMid := middleware.LoggerWithConfig(middleware.LoggerConfig{})
	recoveryMid := middleware.Recovery()

	r.Use(loggerMid, recoveryMid)

	srv.Addr = address
	srv.Handler = r

	go func() {
		log.Println("starting http server, listening on:", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listening %s occur error: %s\n", srv.Addr, err)
		}
	}()
}

// Shutdown http server
func Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalln("cannot shutdown http server:", err)
	}

	// catching ctx.Done(). timeout of 5 seconds.
	select {
	case <-ctx.Done():
		log.Println("shutdown http server timeout of 5 seconds.")
	default:
		log.Println("http server stopped")
	}
}
