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
		sys.POST("/create", createService)
		sys.POST("/cancel/:service", cancelService)
		sys.POST("/cancel/all", cancelAll)
	}

	pprof.Register(r, "/debug/pprof")
}

func stats(c *gin.Context) {
	c.String(200, "pong")
}

/**
eg :
{
	service : "xxxx",
	params:{
		input-kafka-host: "localhost:9092"
    	input-kafka-topic: "goreplay"
    	output-http: ["http://localhost:8002"]
	}
}
*/
type ServiceObj struct {
	Service string          `json:"service"`
	Params  ServiceSettings `json:"params"`
}

// create service
func createService(c *gin.Context) {
	var serviceObj ServiceObj
	errors.Dangerous(c.ShouldBind(&serviceObj))

	renderData(c, serviceObj, nil)

	appPlugins := NewPlugins(serviceObj.Service, serviceObj.Params, nil)
	err := AppEmitter.AddService(serviceObj.Service, appPlugins.Services[serviceObj.Service])
	if err != nil {
		renderData(c, "create service err", err)
	} else {
		renderData(c, "create service success", nil)
	}
}

/**
{service: "XXXX"}
*/
// cancel service
func cancelService(c *gin.Context) {
	service := urlParamStr(c, "service")

	err := AppEmitter.CancelService(service)
	if err != nil {
		renderData(c, "cancel service err", err)
	} else {
		renderData(c, "cancel service success", nil)
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
