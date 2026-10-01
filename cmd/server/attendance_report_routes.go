package main

import (
	"github.com/gin-gonic/gin"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/delivery/http/handler"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/delivery/http/middleware"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/grpcclient"
)

// Keep the route permissions in one registration path used by production and route tests.
//
// Parent reads share the tenant-resource table (KEL-140): GET routes keep
// RequirePermissionForTenantResource so parent tokens reach the ownership
// rules inside the handlers. Mutation routes use
// RequirePermissionForTenantResourceDenyParent, which denies a parent token
// carrying a valid tenant with 403 before identity is consulted, while an
// invalid tenant claim keeps the existing handler 401 path.
func registerAttendanceReportRoutes(api *gin.RouterGroup, client grpcclient.PermissionClient, attendance *handler.AttendanceHandler, report *handler.ReportHandler) {
	api.GET("/attendance", middleware.RequirePermissionForTenantResource(client, "attendance:read"), attendance.List)
	api.POST("/attendance", middleware.RequirePermissionForTenantResourceDenyParent(client, "attendance:create"), attendance.Create)
	api.POST("/attendance/by-session", middleware.RequirePermissionForTenantResourceDenyParent(client, "attendance:create"), attendance.CreateBySession)
	api.POST("/attendance/bulk", middleware.RequirePermissionForTenantResourceDenyParent(client, "attendance:create"), attendance.CreateBulk)
	api.GET("/attendance/by-session", middleware.RequirePermissionForTenantResource(client, "attendance:read"), attendance.GetBySession)
	api.GET("/attendance/:id", middleware.RequirePermissionForTenantResource(client, "attendance:read"), attendance.Get)
	api.PATCH("/attendance/:id", middleware.RequirePermissionForTenantResourceDenyParent(client, "attendance:update"), attendance.Update)
	api.GET("/reports", middleware.RequirePermissionForTenantResource(client, "report:read"), report.List)
	api.POST("/reports", middleware.RequirePermissionForTenantResourceDenyParent(client, "report:create"), report.Create)
	api.GET("/reports/:id", middleware.RequirePermissionForTenantResource(client, "report:read"), report.Get)
	api.PATCH("/reports/:id", middleware.RequirePermissionForTenantResourceDenyParent(client, "report:update"), report.Update)
	api.DELETE("/reports/:id", middleware.RequirePermissionForTenantResourceDenyParent(client, "report:delete"), report.Delete)
}
