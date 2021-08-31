// Gor is simple http traffic replication tool written in Go. Its main goal to replay traffic from production servers to staging and dev environments.
// Now you can test your code on real user sessions in an automated and repeatable fashion.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/mitchellh/mapstructure"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"os/signal"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

var (
	globalservice = "_global_service_"
	AppEmitter    = *NewEmitter()
)

func loggingMiddleware(addr string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/loop" {
			_, err := http.Get("http://" + addr)
			log.Println(err)
		}

		rb, _ := httputil.DumpRequest(r, false)
		log.Println(string(rb))
		next.ServeHTTP(w, r)
	})
}

type FlagSetter interface {
	Set(string) error
}

func MultiOptionDecoder(f reflect.Type, t reflect.Type, data interface{}) (interface{}, error) {
	// fmt.Printf("%v %v %#v \n", f, t, data)

	val := reflect.New(t).Interface()

	if fs, ok := val.(FlagSetter); ok {
		if reflect.TypeOf(data).Kind() == reflect.Slice {
			s := reflect.ValueOf(data)
			for i := 0; i < s.Len(); i++ {
				v := fmt.Sprintf("%v", s.Index(i).Interface())
				if v == "[]" {
					continue
				}

				fs.Set(v)
			}
		} else {
			fs.Set(fmt.Sprintf("%v", data))
		}
		return val, nil
	}

	return data, nil
}

func loadConfig(rawConfig []byte) {
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()
	viper.BindPFlags(pflag.CommandLine)

	viper.SetConfigName("config") // config file name without extension
	viper.SetConfigType("yaml")

	viper.AddConfigPath(".")
	viper.AddConfigPath("/etc/goreplay/")
	viper.AddConfigPath("$HOME/.goreplay")

	viper.SetEnvPrefix("GR")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()

	var err error
	// Used for tests
	if len(rawConfig) > 0 {
		err := viper.ReadConfig(bytes.NewBuffer(rawConfig))
		if err != nil {
			log.Fatal("Error loading config:", err)
		}
	} else {
		// Error can happen if file not found
		err = viper.ReadInConfig()
		if err != nil {
			if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
				log.Fatal("Error loading config:", err)
			}
		}
	}

	err = viper.Unmarshal(&Settings, func(cfg *mapstructure.DecoderConfig) {
		cfg.DecodeHook = mapstructure.ComposeDecodeHookFunc(
			cfg.DecodeHook,
			MultiOptionDecoder,
		)
	})

	if err != nil {
		log.Fatal("Error loading config:", err)
	}
}

func main() {
	if os.Getenv("GOMAXPROCS") == "" {
		runtime.GOMAXPROCS(runtime.NumCPU() * 2)
	}

	args := os.Args[1:]
	var appPlugins = new(AppPlugins)
	if len(args) > 0 && args[0] == "file-server" {
		if len(args) != 2 {
			log.Fatal("You should specify port and IP (optional) for the file server. Example: `gor file-server :80`")
		}
		dir, _ := os.Getwd()

		Debug(0, "Started example file server for current directory on address ", args[1])

		log.Fatal(http.ListenAndServe(args[1], loggingMiddleware(args[1], http.FileServer(http.Dir(dir)))))
		return
	} else {
		// viper.WatchConfig()
		loadConfig(nil)

		appPlugins = NewPlugins(globalservice, Settings.ServiceSettings, nil)
		if len(Settings.Services) > 0 {
			for service, config := range Settings.Services {
				NewPlugins(service, config, appPlugins)
			}
		}
	}

	log.Printf("[PPID %d and PID %d] Version:%s\n", os.Getppid(), os.Getpid(), VERSION)

	// Start emitter
	closeCh := make(chan int)
	go AppEmitter.Start(appPlugins, Settings.Middleware)
	if Settings.ExitAfter > 0 {
		log.Printf("Running gor for a duration of %s\n", Settings.ExitAfter)

		time.AfterFunc(Settings.ExitAfter, func() {
			log.Printf("gor run timeout %s\n", Settings.ExitAfter)
			close(closeCh)
		})
	}

	if Settings.Stats && Settings.HeartBeat != "" {
		heartbeat := NewHeartBeat(Settings.HeartBeat)
		if heartbeat != nil {
			go func(emitter *Emitter, heartbeat *HeartBeat) {
				stat := StatObj{
					Host:    Settings.Address,
					Port:    Settings.Port,
					Version: VERSION,
					AppCode: Settings.AppCode,
				}

				for {
					// wait for output
					time.Sleep(3 * time.Second)

					stat.Stats = emitter.GetStats()
					Debug(0, "stats: ", stats)

					if len(Settings.HeartBeat) > 0 {
						err := heartbeat.reportStat(stat)
						if err != nil {
							Debug(0, "heartbeat stats error,", err)
						}
					}
				}
			}(&AppEmitter, heartbeat)
		}

	}

	// Start http server
	r := gin.New()
	Config(r)
	go Start(r, fmt.Sprintf("%s:%d", Settings.Address, Settings.Port))

	// wait for exit
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	exit := 0
	select {
	case <-c:
		exit = 1
	case <-closeCh:
		exit = 0
	}
	Shutdown()
	AppEmitter.Close()
	os.Exit(exit)
}
