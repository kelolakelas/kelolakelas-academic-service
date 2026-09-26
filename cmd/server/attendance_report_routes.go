package main

import (
	"github.com/gin-gonic/gin"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/delivery/http/handler"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/delivery/http/middleware"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/grpcclient"
)

// Keep the route permissions in one registration path used by production and route tests.
func registerAttendanceReportRoutes(api *gin.RouterGroup, client grpcclient.PermissionClient, attendance *handler.AttendanceHandler, report *handler.ReportHandler) {
	api.GET("/attendance", middleware.RequirePermissionForTenantResource(client, "attendance:read"), attendance.List)
	api.POST("/attendance", middleware.RequirePermissionForTenantResource(client, "attendance:create"), attendance.Create)
	api.GET("/attendance/:id", middleware.RequirePermissionForTenantResource(client, "attendance:read"), attendance.Get)
	api.PATCH("/attendance/:id", middleware.RequirePermissionForTenantResource(client, "attendance:update"), attendance.Update)
	api.GET("/reports", middleware.RequirePermissionForTenantResource(client, "report:read"), report.List)
	api.POST("/reports", middleware.RequirePermissionForTenantResource(client, "report:create"), report.Create)
	api.GET("/reports/:id", middleware.RequirePermissionForTenantResource(client, "report:read"), report.Get)
	api.PATCH("/reports/:id", middleware.RequirePermissionForTenantResource(client, "report:update"), report.Update)
	api.DELETE("/reports/:id", middleware.RequirePermissionForTenantResource(client, "report:delete"), report.Delete)
}
