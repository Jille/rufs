$gittag = &("git.exe") describe --tags --dirty --always
$gittag = $gittag.Trim()
$gitcommit = &("git.exe") log -1 --pretty=format:%H
$gitcommit = $gitcommit.Trim()
@"
// +build withversion

package version
func init() {
	gitTag = "$gittag"
	gitCommit = "$gitcommit"
}
"@ | Out-File -Encoding ASCII -FilePath git_version.go
