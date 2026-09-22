package session

import (
	"path/filepath"
	"time"

	"github.com/douhashi/hikidashi/internal/jsonfile"
)

// Next は 1 セッションの次アクション。JSON の形は sessions/<session_id>.next.json のスキーマで、
// 書き手は抽出プロセス（hikidashi extract）である。
type Next struct {
	// Summary はいま何をしているか（1〜2 文）。
	Summary string `json:"summary"`
	// HumanNext は人間の次アクション。無ければ空。
	HumanNext string `json:"human_next"`
	// ClaudeNext は Claude の次アクション。無ければ空。
	ClaudeNext string `json:"claude_next"`
	// Blockers はブロッカー。
	Blockers []string `json:"blockers"`
	// GeneratedAt は抽出した時刻。
	GeneratedAt time.Time `json:"generated_at"`
}

// ReadNext は引き出し drawerDir にある id の次アクションを返す。
// 未抽出（ファイルが無い）なら ok=false を返す。壊れたファイルも抽出し直すべきものとして、未抽出と同じに扱う。
func ReadNext(drawerDir, id string) (Next, bool, error) {
	return jsonfile.Read[Next](filepath.Join(dir(drawerDir), id+".next.json"))
}
