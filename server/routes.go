package server

import (
	"strconv"

	"github.com/gin-contrib/pprof"
	"github.com/gin-gonic/gin"
	"github.com/toolkits/pkg/errors"
)

// Config routes
func Config(r *gin.Engine) {
	sys := r.Group("/api/")
	{
		sys.GET("/stats", stats)
		sys.POST("/create", createService)
		sys.POST("/cancel", cancelService)
		sys.POST("/cancel/all", cancelAll)
	}

	pprof.Register(r, "/debug/pprof")
}

func stats(c *gin.Context) {
	c.String(200, "pong")
}

/**
{
	service : "xxxx",
	params:{
		input-kafka-host: "localhost:9092"
    	input-kafka-topic: "goreplay"
    	output-http: "http://localhost:8002"
	}
}
*/

type ServiceObj struct {
	service string `json:"service"`
	//params 		goreplay.ServiceSettings 	`json:"params"`
}

func createService(c *gin.Context) {
	var serviceSetting ServiceObj
	errors.Dangerous(c.ShouldBind(&serviceSetting))

	//emitter := goreplay.AppEmitter
	//goreplay.NewPlugins(serviceSetting.service, serviceSetting.params, nil)
	//emitter.StartService(serviceSetting.service, serviceSetting.params ,emitter.Plugins)
}

/**
{service: "XXXX"}
*/
func cancelService(c *gin.Context) {

}

func cancelAll(c *gin.Context) {

}

func urlParamStr(c *gin.Context, field string) string {
	val := c.Param(field)

	if val == "" {
		errors.Bomb("[%s] is blank", field)
	}

	return val
}

func urlParamInt64(c *gin.Context, field string) int64 {
	strval := urlParamStr(c, field)
	intval, err := strconv.ParseInt(strval, 10, 64)
	if err != nil {
		errors.Bomb("cannot convert %s to int64", strval)
	}

	return intval
}

func Message(c *gin.Context, v interface{}) {
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

func Data(c *gin.Context, data interface{}, err error) {
	if err == nil {
		c.JSON(200, gin.H{"dat": data, "err": ""})
		return
	}

	Message(c, err.Error())
}
