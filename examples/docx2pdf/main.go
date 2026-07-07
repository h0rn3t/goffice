// Command docx2pdf converts a .docx file to PDF.
//
// Usage:
//
//	docx2pdf <input.docx> <output.pdf>
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/h0rn3t/docx2pdf/document"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "usage: %s <input.docx> <output.pdf>\n", os.Args[0])
		os.Exit(2)
	}
	in, out := os.Args[1], os.Args[2]

	doc, err := document.Open(in)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = doc.Close() }()

	if err := doc.WritePDF(out); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("wrote %s\n", out)
}
