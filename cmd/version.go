// Copyright 2023 The Okteto Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"fmt"

	"github.com/Masterminds/semver/v3"
	"github.com/okteto/okteto/cmd/utils"
	"github.com/okteto/okteto/pkg/config"
	oktetoLog "github.com/okteto/okteto/pkg/log"
	"github.com/spf13/cobra"
)

// Version returns information about the binary
func Version() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Show the current installed Okteto CLI binary version",
		Args:  utils.NoArgsAccepted("https://okteto.com/docs/reference/okteto-cli/#version"),
		RunE:  Show().RunE,
	}
	cmd.AddCommand(Update())
	cmd.AddCommand(Show())
	return cmd
}

// Update checks if there is a new version available and updates it
func Update() *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Show information about how to update the Okteto CLI binary",
		RunE: func(cmd *cobra.Command, args []string) error {
			currentVersion, err := semver.NewVersion(config.VersionString)
			if err != nil {
				return fmt.Errorf("could not retrieve version")
			}

			if isUpdateAvailable(currentVersion) {
				displayUpdateSteps()
			} else {
				oktetoLog.Success("The latest okteto version is already installed")
			}

			return nil
		},
	}
}

// Show shows the current Okteto CLI version
func Show() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show Okteto CLI version",
		RunE: func(cmd *cobra.Command, args []string) error {
			oktetoLog.Printf("okteto version %s \n", config.VersionString)
			return nil
		},
	}
}

// isUpdateAvailable checks if there is a new version available
func isUpdateAvailable(currentVersion *semver.Version) bool {
	v, err := utils.GetLatestVersionFromGithub()
	if err != nil {
		oktetoLog.Infof("failed to get latest version from github: %s", err)
		return false
	}

	if len(v) > 0 {
		latest, err := semver.NewVersion(v)
		if err != nil {
			oktetoLog.Infof("failed to parse latest version '%s': %s", v, err)
			return false
		}

		if latest.GreaterThan(currentVersion) {
			oktetoLog.Infof("new version available: %s -> %s", currentVersion.String(), latest)
			return true
		}
	}

	return false
}

func displayUpdateSteps() {
	oktetoLog.Println("You can update okteto with the following:")
	oktetoLog.Print(utils.GetUpgradeInstructions())
}
