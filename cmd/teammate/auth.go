// auth.go implements authentication-related subcommands, including login, registration, workspace switching, and other operations.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Authentication operations",
}

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Login with email and password",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		email, _ := cmd.Flags().GetString("email")
		password, _ := cmd.Flags().GetString("password")

		if email == "" || password == "" {
			return fmt.Errorf("both --email and --password are required")
		}

		client := newAPIClientNoAuth()
		var result struct {
			Token     string      `json:"token"`
			ExpiresAt string      `json:"expires_at"`
			Member    interface{} `json:"member"`
		}
		if err := client.Post("/api/auth/login", map[string]string{
			"email":    email,
			"password": password,
		}, &result); err != nil {
			return err
		}

		// Persist credentials
		if err := saveCredentials(&Credentials{
			Token:     result.Token,
			ExpiresAt: parseTime(result.ExpiresAt),
			Email:     email,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to save credentials: %v\n", err)
		}

		fmt.Printf("Login successful, credentials saved to %s\n", credentialsPath())
		return printOutput(result, getOutputFormat())
	},
}

var authRegisterCmd = &cobra.Command{
	Use:   "register",
	Short: "Register a new account",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		name, _ := cmd.Flags().GetString("name")
		email, _ := cmd.Flags().GetString("email")
		password, _ := cmd.Flags().GetString("password")

		if name == "" || email == "" || password == "" {
			return fmt.Errorf("--name, --email, and --password are required")
		}

		client := newAPIClientNoAuth()
		var result struct {
			Token     string      `json:"token"`
			ExpiresAt string      `json:"expires_at"`
			Member    interface{} `json:"member"`
		}
		if err := client.Post("/api/auth/register", map[string]string{
			"name":     name,
			"email":    email,
			"password": password,
		}, &result); err != nil {
			return err
		}

		// Persist credentials
		if err := saveCredentials(&Credentials{
			Token:     result.Token,
			ExpiresAt: parseTime(result.ExpiresAt),
			Email:     email,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to save credentials: %v\n", err)
		}

		fmt.Printf("Registration successful, credentials saved to %s\n", credentialsPath())
		return printOutput(result, getOutputFormat())
	},
}

var authWhoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Show current authenticated user info",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := newAPIClient()
		var result interface{}
		if err := client.Get("/api/auth/whoami", &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Logout (clears stored credentials)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := deleteCredentials(); err != nil {
			return err
		}
		fmt.Println("Logged out, credentials cleared")
		return nil
	},
}

func init() {
	authLoginCmd.Flags().String("email", "", "email address")
	authLoginCmd.Flags().String("password", "", "password")
	authRegisterCmd.Flags().String("name", "", "full name")
	authRegisterCmd.Flags().String("email", "", "email address")
	authRegisterCmd.Flags().String("password", "", "password")

	authCmd.AddCommand(authLoginCmd)
	authCmd.AddCommand(authRegisterCmd)
	authCmd.AddCommand(authWhoamiCmd)
	authCmd.AddCommand(authLogoutCmd)
	rootCmd.AddCommand(authCmd)
}
