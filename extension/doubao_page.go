package extbundle

// DoubaoPageJS is shared by the extension and the desktop result boundary.
func DoubaoPageJS() string {
	page, _ := packed.ReadFile("doubao/page.js")
	return string(page)
}
