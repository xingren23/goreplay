package main

import (
	"github.com/gin-contrib/pprof"
	"github.com/gin-gonic/gin"
	"github.com/toolkits/pkg/errors"
	"time"
)

// Config routes
func Config(r *gin.Engine) {
	sys := r.Group("/api/")
	{
		sys.GET("/stats", stats)
		sys.POST("/create", createTask)
		sys.POST("/cancel/:task", cancelTask)
		sys.POST("/cancel/all", cancelAll)
	}

	pprof.Register(r, "/debug/pprof")
}

func stats(c *gin.Context) {
	stats := AppEmitter.GetStats()
	renderData(c, stats, nil)
}

/**
eg :
{
	task : "xxxx",
	params:{
		input-kafka-host: "localhost:9092"
    	input-kafka-topic: "goreplay"
    	output-http: ["http://localhost:8002"]
	}
}
*/
type TaskObj struct {
	Task   string   `json:"task"`
	Params ParamObj `json:"params"`
}
type ParamObj struct {
	ExitAfter            time.Duration `json:"exit-after"`
	SplitOutput          bool          `json:"split-output"`
	RecognizeTCPSessions bool          `json:"recognize-tcp-sessions"`

	HTTPModifierConfig

	InputFile MultiOption `json:"input-file"`
	FileInputConfig
	OutputFile MultiOption `json:"output-file"`
	FileOutputConfig

	InputRAW MultiOption `json:"input-raw"`
	RAWInputConfig

	InputHTTP MultiOption `json:"input-http"`

	OutputHTTP MultiOption `json:"output-http"`
	HTTPOutputConfig

	InputKafkaConfig
	OutputKafkaConfig
	KafkaTLSConfig
}

// create task
func createTask(c *gin.Context) {
	var taskObj TaskObj
	errors.Dangerous(c.ShouldBind(&taskObj))

	serviceSettings := ServiceSettings{
		ExitAfter:            taskObj.Params.ExitAfter,
		SplitOutput:          taskObj.Params.SplitOutput,
		RecognizeTCPSessions: taskObj.Params.RecognizeTCPSessions,
		HTTPModifierConfig:   taskObj.Params.HTTPModifierConfig,
		InputFile:            taskObj.Params.InputFile,
		InputFileConfig:      taskObj.Params.FileInputConfig,
		OutputFile:           taskObj.Params.OutputFile,
		OutputFileConfig:     taskObj.Params.FileOutputConfig,
		InputRAW:             taskObj.Params.InputRAW,
		InputRAWConfig:       taskObj.Params.RAWInputConfig,
		InputHTTP:            taskObj.Params.InputHTTP,
		OutputHTTP:           taskObj.Params.OutputHTTP,
		OutputHTTPConfig:     taskObj.Params.HTTPOutputConfig,
		InputKafkaConfig:     taskObj.Params.InputKafkaConfig,
		OutputKafkaConfig:    taskObj.Params.OutputKafkaConfig,
		KafkaTLSConfig:       taskObj.Params.KafkaTLSConfig,
	}

	appPlugins := NewPlugins(taskObj.Task, serviceSettings, nil)
	err := AppEmitter.AddService(taskObj.Task, appPlugins.Services[taskObj.Task])
	if err != nil {
		renderData(c, "create task err", err)
	} else {
		renderData(c, "create task success", nil)
	}
}

/**
{service: "XXXX"}
*/
// cancel task
func cancelTask(c *gin.Context) {
	task := urlParamStr(c, "task")

	err := AppEmitter.CancelService(task)
	if err != nil {
		renderData(c, "cancel task err", err)
	} else {
		renderData(c, "cancel task success", nil)
	}
}

// cancel all service
func cancelAll(c *gin.Context) {

}

func urlParamStr(c *gin.Context, field string) string {
	val := c.Param(field)

	if val == "" {
		errors.Bomb("[%s] is blank", field)
	}

	return val
}

func renderMessage(c *gin.Context, v interface{}) {
	if v == nil {
		c.JSON(200, gin.H{"err": ""})
		return
	}

	switch t := v.(type) {
	case string:
		c.JSON(200, gin.H{"err": t})
	case error:
		c.JSON(200, gin.H{"err": t.Error()})
	}
}

func renderData(c *gin.Context, data interface{}, err error) {
	if err == nil {
		c.JSON(200, gin.H{"dat": data, "err": ""})
		return
	}

	renderMessage(c, err.Error())
}
