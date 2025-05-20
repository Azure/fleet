package main

import (
	"fmt"
	"os"

	"go.goms.io/fleet/pkg/authtoken/providers/azure"
)

func main() {
	// Parse command line arguments
	clientID := "test-client-id"
	directScope := ""
	
	if len(os.Args) > 1 {
		clientID = os.Args[1]
	}
	
	if len(os.Args) > 2 {
		directScope = os.Args[2]
	}

	// Get the environment variable
	aksScope := os.Getenv("AKS_SCOPE")
	fmt.Printf("AKS_SCOPE environment variable: %s\n", aksScope)

	// Create Azure MSI auth provider
	provider := azure.New(clientID, directScope)
	
	// Check if it's the correct type
	if azProvider, ok := provider.(*azure.AuthTokenProvider); ok {
		fmt.Printf("Configured scope in Azure MSI auth provider: %s\n", azProvider.Scope)
		
		// Verify the scope matches the expected values based on the configuration hierarchy
		if directScope != "" && azProvider.Scope == directScope {
			fmt.Println("Success: Provider is using the direct scope parameter")
		} else if directScope == "" && aksScope != "" && azProvider.Scope == aksScope {
			fmt.Println("Success: Provider is using the scope from environment variable")
		} else if directScope == "" && aksScope == "" && azProvider.Scope == azure.GetDefaultAKSScope() {
			fmt.Println("Success: Provider is using the default scope")
		} else {
			fmt.Printf("Error: Unexpected scope configuration. DirectScope=%q, EnvScope=%q, ActualScope=%q\n", 
				directScope, aksScope, azProvider.Scope)
			os.Exit(1)
		}
	} else {
		fmt.Println("Error: Provider is not of type *AuthTokenProvider")
		os.Exit(1)
	}
}