package notify

import (
	"fmt"
	"runtime"
	"time"
)

// GetSystemStats reads Go runtime memory statistics to help identify memory leaks or high allocations.
func GetSystemStats(interval time.Duration, checkCount int64) string {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	allocMB := float64(m.Alloc) / 1024 / 1024
	sysMB := float64(m.Sys) / 1024 / 1024
	routines := runtime.NumGoroutine()

	return fmt.Sprintf("📊 *Sistem Durumu*\n\n"+
		"• *Kontrol Aralığı:* %.0f Dakika\n"+
		"• *Toplam Kontrol Sayısı:* %d\n"+
		"• *Aktif Goroutine:* %d\n"+
		"• *Kullanılan Bellek (Alloc):* %.2f MB\n"+
		"• *İşletim Sisteminden Ayrılan (Sys):* %.2f MB\n"+
		"• *Toplam GC Döngüsü:* %d",
		interval.Minutes(), checkCount, routines, allocMB, sysMB, m.NumGC)
}
