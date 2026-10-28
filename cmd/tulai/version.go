package main

// ProductVersion 是桌面端产品版本。go tool mygo build 还会把 mygo.json 的
// version 链进二进制；TestProductVersionMatchesConfig 要求两边相同。
// MV3 页面桥（extension/manifest.json）单独编号，不是这个版本。
const ProductVersion = "0.1.6"
