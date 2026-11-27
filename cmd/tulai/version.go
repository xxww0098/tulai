package main

// ProductVersion 是桌面端产品版本。go tool mygo build 还会把根目录 mygo.json 的
// version 链进二进制；TestProductVersionMatchesConfig 要求两边相同。
const ProductVersion = "0.1.42"
