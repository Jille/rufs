gittag=`git describe --tags --dirty --always`
gitcommit=`git log -1 --pretty='format:%H'`
cat <<EOF >git_version.go
// +build withversion

package version
func init() {
	gitTag = "$gittag"
	gitCommit = "$gitcommit"
}
EOF
