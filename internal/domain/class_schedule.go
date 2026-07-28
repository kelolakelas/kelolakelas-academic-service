package domain

import (
	"github.com/google/uuid"
)

type ClassSchedule struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ClassID   uuid.UUID `gorm:"type:uuid;not null;index:idx_class_day" json:"class_id"`
	DayOfWeek int       `gorm:"type:integer;not null;index:idx_class_day" json:"day_of_week"` // 1 = Senin, 7 = Minggu
	StartTime string    `gorm:"type:time;not null" json:"start_time"`                         // standard HH:MM:SS for time column
	EndTime   string    `gorm:"type:time;not null" json:"end_time"`                           // standard HH:MM:SS for time column

	Class *Class `gorm:"foreignKey:ClassID" json:"class,omitempty"`
}
