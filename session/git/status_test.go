package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParsePorcelainZ(t *testing.T) {
	out := "MM both.go\x00 M changed.go\x00A  added.go\x00?? new dir/file.txt\x00R  new.go\x00old.go\x00 D gone.go\x00"
	files := parsePorcelainZ(out)
	require.Len(t, files, 6)

	require.Equal(t, "both.go", files[0].Path)
	require.True(t, files[0].Staged())
	require.True(t, files[0].Changed())

	require.False(t, files[1].Staged())
	require.True(t, files[1].Changed())

	require.True(t, files[2].Staged())
	require.False(t, files[2].Changed())

	require.Equal(t, "new dir/file.txt", files[3].Path)
	require.True(t, files[3].Untracked())
	require.False(t, files[3].Staged())
	require.True(t, files[3].Changed())

	require.Equal(t, "new.go", files[4].Path)
	require.Equal(t, "old.go", files[4].Orig)
	require.True(t, files[4].Staged())

	require.Equal(t, "gone.go", files[5].Path)
	require.Equal(t, byte('D'), files[5].Y)
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

func TestStageUnstageDiscardCommit(t *testing.T) {
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")

	// Unborn HEAD: stage then unstage must work without a commit.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0644))
	require.NoError(t, Stage(dir, "a.txt"))
	files, err := Status(dir)
	require.NoError(t, err)
	require.True(t, files[0].Staged())
	require.NoError(t, Unstage(dir, "a.txt"))
	files, _ = Status(dir)
	require.True(t, files[0].Untracked())

	// Commit.
	require.NoError(t, Stage(dir))
	require.Error(t, Commit(dir, "  "))
	gitIn(t, dir, "commit", "-q", "-m", "first")

	// Modify, view diff, discard.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("two\n"), 0644))
	files, _ = Status(dir)
	d, err := FileDiff(dir, files[0], false)
	require.NoError(t, err)
	require.True(t, strings.Contains(d, "+two"))
	require.NoError(t, Discard(dir, files[0]))
	files, _ = Status(dir)
	require.Empty(t, files)

	// Untracked: diff shows the whole file, discard deletes it.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("new\n"), 0644))
	files, _ = Status(dir)
	d, err = FileDiff(dir, files[0], false)
	require.NoError(t, err)
	require.True(t, strings.Contains(d, "+new"))
	require.NoError(t, Discard(dir, files[0]))
	_, err = os.Stat(filepath.Join(dir, "b.txt"))
	require.True(t, os.IsNotExist(err))
}
