package server

import (
	"github.com/Hoverhuang-er/overlord/platform/api/model"
	"github.com/Hoverhuang-er/overlord/platform/job"
	"net/http"

	"github.com/gin-gonic/gin"
)

// GET /clusters/:cluster_id/instances
func getInstances(c *gin.Context) {
}

func changeInstanceWeight(c *gin.Context) {
	p := model.ParamScaleWeight{}
	if err := c.ShouldBind(&p); err != nil {
		eJSON(c, err)
		return
	}
	instance := c.Param("instance_addr")

	err := svc.SetInstanceWeight(instance, p.Weight)
	if err != nil {
		eJSON(c, err)
		return
	}
	done(c)
}

func restartInstance(c *gin.Context) {
	cname := c.Param("cluster_name")
	addr := c.Param("instance_addr")
	jobid, err := svc.RestartInstance(cname, addr)
	if err != nil {
		eJSON(c, err)
		return
	}
	c.JSON(http.StatusOK, &model.Job{ID: jobid, State: job.StatePending})
}
