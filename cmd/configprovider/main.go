/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/
package main

import (
	"github.com/codegangsta/negroni"
	"github.com/gorilla/mux"

	"go.goms.io/fleet/pkg/configprovider"
)

func main() {
	r := mux.NewRouter()
	r.Path("/refreshtoken").Methods("POST").HandlerFunc(configprovider.CheckToken)
	n := negroni.Classic()
	n.UseHandler(r)
	n.Run(":4000")
}
