// Package repopath builds repository-relative paths for capture files.
//
// The custom file webhook writes every note and attachment into a required
// capture folder. The folder name is the stable capture key, while each file name
// is preserved exactly after validation.
package repopath
