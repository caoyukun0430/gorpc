package gorpc

import (
	"fmt"
	"html/template"
	"net/http"
)

// design a html to expose the service methods call conditions, method vs #calls
// exposed in /debug/methods

const debugText = `<html>
	<body>
	<title>GeeRPC Services</title>
	{{range .}}
	<hr>
	Service {{.Name}}
	<hr>
		<table>
		<th align=center>Method</th><th align=center>Calls</th>
		{{range $name, $mtype := .Method}}
			<tr>
			<td align=left font=fixed>{{$name}}({{$mtype.ArgType}}, {{$mtype.ReplyType}}) error</td>
			<td align=center>{{$mtype.NumCalls}}</td>
			</tr>
		{{end}}
		</table>
	{{end}}
	</body>
	</html>`

var debug = template.Must(template.New("RPC debug").Parse(debugText))

//	type Server struct {
//		//  store key-value pairs and ensures thread-safe access to these pairs
//		serviceMap sync.Map
//	}
type debugHTTP struct {
	*Server //
}

type debugService struct {
	Name      string
	MethodMap map[string]*methodType
}

// Runs at /debug/methods
func (server debugHTTP) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// Build a sorted version of the data.
	var services []debugService
	// The Range method takes a function as an argument and applies this function to each key-value pair in the map, if return true, iteration continues
	server.serviceMap.Range(func(namei, svci interface{}) bool {
		svc := svci.(*service)
		services = append(services, debugService{
			Name:      namei.(string),
			MethodMap: svc.methodMap,
		})
		return true
	})
	// The Execute method applies the template to the data (services in this case) and writes the output to the specified io.Writer (w)
	err := debug.Execute(w, services)
	if err != nil {
		_, _ = fmt.Fprintln(w, "rpc: error executing template:", err.Error())
	}
}
