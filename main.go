package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-openapi/spec"
	"github.com/richardwilkes/toolbox/cmdline"
	"github.com/richardwilkes/toolbox/errs"
	"github.com/swaggo/swag"
)

func main() {
	cmdline.AppName = "Swagger Doc"
	cmdline.AppVersion = "2.3.5"
	cmdline.CopyrightStartYear = "2019"
	cmdline.CopyrightHolder = "Richard A. Wilkes"

	cl := cmdline.New(true)
	apiDir := "api"
	searchDir := "."
	mainAPIFile := "main.go"
	destDir := "docs"
	baseName := "swagger"
	maxDependencyDepth := 2
	markdownFileDir := ""
	title := ""
	serverURL := ""
	embedded := false
	useOldMethod := false
	tags := ""
	firstTagOnly := false
	generateHtml := true
	badges := ""

	var exclude []string
	cl.NewGeneralOption(&searchDir).SetSingle('s').SetName("search").SetArg("dir").SetUsage("The directory root to search for documentation directives")
	cl.NewGeneralOption(&mainAPIFile).SetSingle('m').SetName("main").SetArg("file").SetUsage("The Go file to search for the main documentation directives")
	cl.NewGeneralOption(&destDir).SetSingle('o').SetName("output").SetArg("dir").SetUsage("The destination directory to write the documentation files to")
	cl.NewGeneralOption(&apiDir).SetSingle('a').SetName("api").SetArg("dir").SetUsage("The intermediate directory within the output directory to write the files to")
	cl.NewGeneralOption(&baseName).SetSingle('n').SetName("name").SetArg("name").SetUsage("The base name to use for the definition files")
	cl.NewGeneralOption(&maxDependencyDepth).SetSingle('d').SetName("depth").SetUsage("The maximum depth to resolve dependencies; use 0 for unlimited (only used if --old-method is set)")
	cl.NewGeneralOption(&markdownFileDir).SetSingle('i').SetName("mdincludes").SetArg("dir").SetUsage("The directory root to search for markdown includes")
	cl.NewGeneralOption(&title).SetSingle('t').SetName("title").SetArg("text").SetUsage("The title for the HTML page. If unset, defaults to the base name")
	cl.NewGeneralOption(&serverURL).SetSingle('u').SetName("url").SetArg("url").SetUsage("An additional server URL")
	cl.NewGeneralOption(&embedded).SetSingle('e').SetName("embedded").SetUsage("When set, embeds the spec directly in the html")
	cl.NewGeneralOption(&useOldMethod).SetName("old-method").SetUsage("Use old method for parsing dependencies")
	cl.NewGeneralOption(&exclude).SetSingle('x').SetName("exclude").SetUsage("Exclude directories and files when searching. Example for multiple: -x file1 -x file2")
	cl.NewGeneralOption(&tags).SetSingle('g').SetName("tags").SetArg("tag1,tag2").SetUsage("A comma-separated list of tags to filter the APIs. Prefix with '!' to exclude APIs with that tag")
	cl.NewGeneralOption(&firstTagOnly).SetSingle('f').SetName("firstTagOnly").SetUsage("Keep only the first tag in the list of tags for each API. This is useful for generating a single-page API documentation.")
	cl.NewGeneralOption(&generateHtml).SetSingle('l').SetName("generateHtml").SetUsage("When set, embeds the spec directly in the html")
	cl.NewGeneralOption(&badges).SetSingle('b').SetName("badges").SetArg("tag:color,...").SetUsage("Comma-separated list of tag:color pairs to generate badges")

	cl.Parse(os.Args[1:])
	if title == "" {
		title = baseName
	}
	if err := generate(
		searchDir,
		mainAPIFile,
		destDir,
		apiDir,
		baseName,
		title,
		serverURL,
		tags,
		markdownFileDir,
		exclude,
		maxDependencyDepth,
		embedded,
		useOldMethod,
		firstTagOnly,
		generateHtml,
		badges,
	); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func generate(searchDir,
	mainAPIFile,
	destDir,
	apiDir,
	baseName,
	title,
	serverURL,
	tags,
	markdownFileDir string,
	exclude []string,
	maxDependencyDepth int,
	embedded,
	useOldMethod,
	firstTagOnly,
	generateHtml bool,
	badges string,
) error {

	if err := os.MkdirAll(filepath.Join(destDir, apiDir), 0o755); err != nil {
		return errs.Wrap(err)
	}

	opts := make([]func(*swag.Parser), 0)
	if len(exclude) != 0 {
		opts = append(opts, swag.SetExcludedDirsAndFiles(strings.Join(exclude, ",")))
	}
	if markdownFileDir != "" {
		opts = append(opts, swag.SetMarkdownFileDirectory(markdownFileDir))
	}
	opts = append(opts,
		swag.ParseUsingGoList(!useOldMethod),
		swag.SetDebugger(&filter{out: log.New(os.Stdout, "", log.LstdFlags)}),
	)
	if tags != "" {
		opts = append(opts, swag.SetTags(tags))
	}

	parser := swag.New(opts...)
	parser.ParseDependency = swag.ParseModels
	parser.ParseInternal = true
	if err := parser.ParseAPI(searchDir, mainAPIFile, maxDependencyDepth); err != nil {
		return errs.Wrap(err)
	}
	swagger := parser.GetSwagger()

	badgeMap := make(map[string]string)
	if badges != "" {
		for _, pair := range strings.Split(badges, ",") {
			parts := strings.Split(pair, ":")
			if len(parts) == 2 {
				tag := strings.TrimSpace(parts[0])
				color := strings.TrimSpace(parts[1])
				badgeMap[tag] = color
			}
		}
	}

	for path, pathItem := range swagger.Paths.Paths {
		operations := []*spec.Operation{
			pathItem.Get, pathItem.Post, pathItem.Put, pathItem.Delete,
			pathItem.Options, pathItem.Head, pathItem.Patch,
		}
		opRefs := []*spec.Operation{pathItem.Get, pathItem.Post, pathItem.Put, pathItem.Delete, pathItem.Options, pathItem.Head, pathItem.Patch}

		for i, operation := range operations {

			if operation == nil || len(operation.Tags) == 0 {
				continue
			}

			log.Printf("Processing %s [%s]: Tags: %v\n", path, operation.Tags)

			var badgeList []map[string]string
			for _, tag := range operation.Tags {
				if color, ok := badgeMap[tag]; ok {
					badgeList = append(badgeList, map[string]string{
						"label": tag,
						"color": color,
					})
				}
			}
			if len(badgeList) > 0 {
				if operation.VendorExtensible.Extensions == nil {
					operation.VendorExtensible.Extensions = make(spec.Extensions)
				}
				operation.VendorExtensible.Extensions["x-badges"] = badgeList
			}

			// Limit tags to first if --firstTagOnly is set
			if firstTagOnly && len(operation.Tags) > 0 {
				operation.Tags = []string{operation.Tags[0]}
			}

			opRefs[i] = operation
		}

		// Reassign back modified operations
		pathItem.Get, pathItem.Post, pathItem.Put, pathItem.Delete,
			pathItem.Options, pathItem.Head, pathItem.Patch = opRefs[0], opRefs[1], opRefs[2], opRefs[3], opRefs[4], opRefs[5], opRefs[6]

		swagger.Paths.Paths[path] = pathItem
	}

	jData, err := json.MarshalIndent(swagger, "", "  ")
	if err != nil {
		return errs.Wrap(err)
	}
	if err = os.WriteFile(filepath.Join(destDir, apiDir, baseName+".json"), jData, 0o644); err != nil {
		return errs.Wrap(err)
	}
	var specURL, extra, js string
	if serverURL != "" {
		extra = fmt.Sprintf(`
          server-url="%s"`, serverURL)
	}
	if embedded {
		js = fmt.Sprintf(`
<script>
    window.addEventListener("DOMContentLoaded", (event) => {
        const rapidocEl = document.getElementById("rapidoc");
        rapidocEl.loadSpec(%s)
    })
</script>`, string(jData))
	} else {
		specURL = fmt.Sprintf(`
          spec-url="%s.json"`, baseName)
	}
	if generateHtml {
		if err = os.WriteFile(filepath.Join(destDir, apiDir, "index.html"), []byte(fmt.Sprintf(`<!doctype html>
<html>
<head>
    <meta charset="utf-8">
	<title>%s</title>
	<script src="https://cdnjs.cloudflare.com/ajax/libs/rapidoc/9.3.8/rapidoc-min.js"
			integrity="sha512-0ES6eX4K9J1PrIEjIizv79dTlN5HwI2GW9Ku6ymb8dijMHF5CIplkS8N0iFJ/wl3GybCSqBJu8HDhiFkZRAf0g=="
			crossorigin="anonymous"
			referrerpolicy="no-referrer">
	</script>
</head>
<body>
<rapi-doc id="rapidoc"
          theme="dark"
          render-style="read"
          schema-style="table"
          schema-description-expanded="true"%s
          allow-spec-file-download="true"%s
>
</rapi-doc>%s
</body>
</html>`, title, specURL, extra, js)), 0o644); err != nil {
			return errs.Wrap(err)
		}
	}
	return nil
}

type filter struct {
	out *log.Logger
}

func (f *filter) Printf(format string, v ...interface{}) {
	s := fmt.Sprintf(format, v...)
	if strings.Contains(s, "warning: failed to evaluate const mProfCycleWrap") {
		return
	}
	f.out.Println(s)
}
