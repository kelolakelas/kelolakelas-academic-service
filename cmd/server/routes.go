package main

import (
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	"github.com/kelolakelas/kelolakelas-academic-service/internal/delivery/http/handler"
	"github.com/kelolakelas/kelolakelas-academic-service/internal/delivery/http/middleware"
	"github.com/kelolakelas/kelolakelas-academic-service/pkg/grpcclient"
)

// routeHandlers carries every handler the HTTP surface serves.
type routeHandlers struct {
	health         gin.HandlerFunc
	ready          gin.HandlerFunc
	catalog        *handler.CatalogHandler
	category       *handler.CategoryHandler
	class          *handler.ClassHandler
	list           *handler.ListHandler
	student        *handler.StudentHandler
	attendance     *handler.AttendanceHandler
	report         *handler.ReportHandler
	enrollment     *handler.EnrollmentHandler
	privateRequest *handler.PrivateScheduleRequestHandler
	schedule       *handler.ScheduleHandler
	session        *handler.SessionHandler
}

// registerRoutes is the single route table used by production and by the Swagger
// coverage test, so the published contract is checked against what actually runs.
// Each permission-guarded route must carry the same permission in its handler's
// `@x-permission` annotation (see swagger_contract_test.go).
func registerRoutes(r *gin.Engine, h routeHandlers, jwtSecret, internalCredential string, permissionClient grpcclient.PermissionClient) {
	r.GET("/health", h.health)
	r.GET("/ready", h.ready)

	// Swagger UI
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	apiV1 := r.Group("/api/v1")
	apiV1.GET("/catalog/classes", h.catalog.ListClasses)
	apiV1.GET("/catalog/classes/:id", h.catalog.GetClass)
	apiV1.Use(middleware.AuthMiddleware(jwtSecret))
	{
		apiV1.GET("/categories", h.list.ListCategories)
		apiV1.POST("/categories", middleware.RequirePermission(permissionClient, "category:create"), h.category.Create)
		apiV1.DELETE("/categories/:id", middleware.RequirePermission(permissionClient, "category:delete"), h.category.Delete)
		apiV1.GET("/classes", h.list.ListClasses)
		apiV1.POST("/classes", middleware.RequirePermission(permissionClient, "class:create"), h.class.Create)
		apiV1.POST("/classes/with-category", middleware.RequirePermission(permissionClient, "class:create"), h.class.CreateWithCategory)
		apiV1.DELETE("/classes/:id", middleware.RequirePermission(permissionClient, "class:delete"), h.class.Delete)
		apiV1.PATCH("/classes/:id", middleware.RequirePermission(permissionClient, "class:update"), h.class.Update)
		apiV1.PATCH("/classes/:id/published", middleware.RequirePermission(permissionClient, "class:update"), h.class.UpdatePublication)
		apiV1.GET("/schedules", h.list.ListSchedules)
		// Student and enrollment routes are shared by tenant members and parents. Parents
		// hold ownership rather than a role, so the permission check applies only to
		// non-parent callers; the owned-resource rules inside each handler still decide
		// what a parent may reach. See ADR 0002.
		apiV1.GET("/students", middleware.RequirePermissionUnlessParent(permissionClient, "student:read"), h.student.List)
		apiV1.POST("/students", middleware.RequirePermissionUnlessParent(permissionClient, "student:create"), h.student.Create)
		apiV1.GET("/students/:id", middleware.RequirePermissionUnlessParent(permissionClient, "student:read"), h.student.Get)
		apiV1.PATCH("/students/:id", middleware.RequirePermissionUnlessParent(permissionClient, "student:update"), h.student.Update)
		apiV1.DELETE("/students/:id", middleware.RequirePermissionUnlessParent(permissionClient, "student:delete"), h.student.Delete)
		registerAttendanceReportRoutes(apiV1, permissionClient, h.attendance, h.report)
		apiV1.POST("/tenants/:tenant_id/enrollments", middleware.RequirePermissionUnlessParent(permissionClient, "enrollment:create"), h.enrollment.Create)
		apiV1.POST("/catalog/classes/:class_id/enrollments", h.enrollment.CreateCatalogEnrollment)
		apiV1.POST("/enrollments/:id/cancel", h.enrollment.Cancel)
		apiV1.GET("/enrollments", middleware.RequirePermissionUnlessParent(permissionClient, "enrollment:read"), h.enrollment.ListQuery)
		apiV1.GET("/enrollments/:id", middleware.RequirePermissionUnlessParent(permissionClient, "enrollment:read"), h.enrollment.GetQuery)
		apiV1.PATCH("/enrollments/:id/schedule", h.enrollment.AssignSchedule)
		apiV1.POST("/catalog/classes/:class_id/schedule-requests", h.privateRequest.Create)
		apiV1.GET("/schedule-requests", middleware.RequirePermissionUnlessParent(permissionClient, "enrollment:read"), h.privateRequest.List)
		apiV1.GET("/schedule-requests/:id", middleware.RequirePermissionUnlessParent(permissionClient, "enrollment:read"), h.privateRequest.Get)
		apiV1.POST("/schedule-requests/:id/approve", middleware.RequirePermission(permissionClient, "enrollment:update"), h.privateRequest.Approve)
		apiV1.POST("/schedule-requests/:id/reject", middleware.RequirePermission(permissionClient, "enrollment:update"), h.privateRequest.Reject)
		apiV1.POST("/schedule-requests/:id/recommendation/accept", h.privateRequest.AcceptRecommendation)
		apiV1.POST("/schedule-requests/:id/recommendation/decline", h.privateRequest.DeclineRecommendation)
		apiV1.POST("/schedule-requests/:id/cancel", h.privateRequest.Cancel)

		// Schedule Routes
		apiV1.POST("/schedules", middleware.RequirePermission(permissionClient, "schedule:create"), h.schedule.CreateInitialSchedules)
		apiV1.DELETE("/schedules/:id", middleware.RequirePermission(permissionClient, "schedule:delete"), h.schedule.Delete)
		apiV1.PUT("/schedules/permanent", middleware.RequirePermission(permissionClient, "schedule:update"), h.schedule.ChangeSchedulePermanent)
		apiV1.PUT("/schedules/:id/permanent", middleware.RequirePermission(permissionClient, "schedule:update"), h.schedule.ChangeSchedulePermanent)
		apiV1.PATCH("/schedules/tutor-permanent", middleware.RequirePermission(permissionClient, "schedule:update"), h.schedule.ChangeTutorPermanent)
		apiV1.PATCH("/schedules/:id/tutor-permanent", middleware.RequirePermission(permissionClient, "schedule:update"), h.schedule.ChangeTutorPermanent)
		apiV1.PUT("/schedules/tutor-permanent", middleware.RequirePermission(permissionClient, "schedule:update"), h.schedule.ChangeTutorPermanent)
		apiV1.PUT("/schedules/:id/tutor-permanent", middleware.RequirePermission(permissionClient, "schedule:update"), h.schedule.ChangeTutorPermanent)

		// Session Routes
		apiV1.GET("/sessions", h.session.ListSessions)
		apiV1.GET("/sessions/:id", h.session.GetSession)
		apiV1.DELETE("/sessions/:id", middleware.RequirePermission(permissionClient, "schedule:update"), h.session.DeleteSession)
		apiV1.POST("/sessions/reschedule", middleware.RequirePermission(permissionClient, "schedule:update"), h.schedule.RescheduleSession)
		apiV1.POST("/sessions/:id/reschedule", middleware.RequirePermission(permissionClient, "schedule:update"), h.schedule.RescheduleSession)
		apiV1.PATCH("/sessions/substitute-tutor", middleware.RequirePermission(permissionClient, "schedule:update"), h.schedule.ChangeTutorTemporary)
		apiV1.PATCH("/sessions/:id/substitute-tutor", middleware.RequirePermission(permissionClient, "schedule:update"), h.schedule.ChangeTutorTemporary)
		apiV1.GET("/sessions/:id/attendees", h.schedule.GetSessionAttendees)
	}
	internal := r.Group("/internal")
	internal.Use(middleware.InternalServiceAuth(internalCredential))
	internal.PUT("/enrollments/:id/activate", h.enrollment.ActivateInternal)
	internal.PUT("/enrollments/:id/release", h.enrollment.ReleaseInternal)
}
