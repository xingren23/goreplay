package main

import (
	"github.com/gin-contrib/pprof"
	"github.com/gin-gonic/gin"
	"github.com/toolkits/pkg/errors"
)

// Config routes
func Config(r *gin.Engine) {
	sys := r.Group("/api/")
	{
		sys.GET("/stats", stats)
		sys.POST("/create", createTask)
		sys.POST("/cancel/:id", cancelTask)
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
	Task   string          `json:"task"`
	Params ServiceSettings `json:"params"`
}

// create task
func createTask(c *gin.Context) {
	var taskObj TaskObj
	errors.Dangerous(c.ShouldBind(&taskObj))

	appPlugins := NewPlugins(taskObj.Task, taskObj.Params, nil)
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
