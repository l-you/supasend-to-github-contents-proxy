package repopath

import "path"

func File(noteDir string, folderName string, fileName string) string {
	return path.Join(noteDir, folderName, fileName)
}
