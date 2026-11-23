package extbundle

// DoubaoPageJS is shared by the extension and the desktop result boundary.
func DoubaoPageJS() string {
	page, _ := packed.ReadFile("doubao/page.js")
	return string(page)
}

// DoubaoSourceMapJS matches generated images to this turn's uploaded attachments.
func DoubaoSourceMapJS() string {
	script, _ := packed.ReadFile("doubao/source_map.js")
	return string(script)
}
