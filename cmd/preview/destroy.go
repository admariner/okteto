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

package preview

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"time"

	contextCMD "github.com/okteto/okteto/cmd/context"
	"github.com/okteto/okteto/cmd/utils"
	"github.com/okteto/okteto/pkg/analytics"
	"github.com/okteto/okteto/pkg/constants"
	oktetoErrors "github.com/okteto/okteto/pkg/errors"
	oktetoLog "github.com/okteto/okteto/pkg/log"
	"github.com/okteto/okteto/pkg/okteto"
	"github.com/okteto/okteto/pkg/types"
	"github.com/spf13/cobra"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type destroyPreviewCommand struct {
	okClient  types.OktetoInterface
	k8sClient kubernetes.Interface
}

func newDestroyPreviewCommand(okClient types.OktetoInterface, k8sClient kubernetes.Interface) destroyPreviewCommand {
	return destroyPreviewCommand{
		okClient,
		k8sClient,
	}
}

type DestroyOptions struct {
	name       string
	k8sContext string
	wait       bool
	timeout    time.Duration
}

// Destroy destroy a preview
func Destroy(ctx context.Context) *cobra.Command {
	opts := &DestroyOptions{}
	cmd := &cobra.Command{
		Use:   "destroy <name>",
		Short: "Destroy a Preview Environment",
		Args:  utils.ExactArgsAccepted(1, ""),
		Example: `To destroy a Preview Environment without the Okteto CLI wait for its completion, use the '--wait=false' flag:
okteto preview destroy --wait=false`,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.name = getExpandedName(args[0])

			if err := contextCMD.NewContextCommand().Run(ctx, &contextCMD.Options{Show: true, Context: opts.k8sContext}); err != nil {
				return err
			}

			if !okteto.IsOkteto() {
				return oktetoErrors.ErrContextIsNotOktetoCluster
			}

			oktetoClient, err := okteto.NewOktetoClient()
			if err != nil {
				return err
			}
			k8sClient, _, err := okteto.GetK8sClient()
			if err != nil {
				return err
			}
			c := newDestroyPreviewCommand(oktetoClient, k8sClient)

			err = c.executeDestroyPreview(ctx, opts)
			analytics.TrackPreviewDestroy(err == nil)
			return err
		},
	}
	cmd.Flags().StringVarP(&opts.k8sContext, "context", "c", "", "overwrite the current Okteto Context")
	cmd.Flags().BoolVarP(&opts.wait, "wait", "w", true, "wait until the Preview Environment destruction finishes")
	cmd.Flags().DurationVarP(&opts.timeout, "timeout", "t", fiveMinutes, "the duration to wait for the Preview Environment to be destroyed. Any value should contain a corresponding time unit e.g. 1s, 2m, 3h")
	return cmd
}

func (c destroyPreviewCommand) executeDestroyPreview(ctx context.Context, opts *DestroyOptions) error {
	oktetoLog.Spinner(fmt.Sprintf("Destroying %q preview environment", opts.name))
	oktetoLog.StartSpinner()
	defer oktetoLog.StopSpinner()

	if err := c.okClient.Previews().Destroy(ctx, opts.name, int(opts.timeout.Seconds())); err != nil {
		if oktetoErrors.IsNotFound(err) {
			oktetoLog.Information("Preview environment '%s' not found.", opts.name)
			return nil
		}
		return fmt.Errorf("failed to destroy preview environment: %w", err)
	}

	if !opts.wait {
		oktetoLog.Success("Preview environment '%s' scheduled to destroy", opts.name)
		return nil
	}

	if err := c.watchDestroy(ctx, opts.name, opts.timeout); err != nil {
		return err
	}

	oktetoLog.Success("Preview environment '%s' successfully destroyed", opts.name)
	return nil
}

func (c destroyPreviewCommand) watchDestroy(ctx context.Context, preview string, timeout time.Duration) error {
	waitCtx, ctxCancel := context.WithCancel(ctx)
	defer ctxCancel()

	stop := make(chan os.Signal, 1)
	defer close(stop)

	signal.Notify(stop, os.Interrupt)
	exit := make(chan error, 1)
	defer close(exit)

	var wg sync.WaitGroup

	wg.Add(1)
	go func(wg *sync.WaitGroup) {
		defer wg.Done()
		exit <- c.waitForPreviewDestroyed(waitCtx, preview, timeout)
	}(&wg)

	wg.Add(1)
	go func(wg *sync.WaitGroup) {
		defer wg.Done()
		err := c.okClient.Stream().DestroyAllLogs(waitCtx, preview)
		// Check if error is not canceled because in the case of a timeout waiting the operation to complete,
		// we cancel the context to stop streaming logs, but we should not display the warning
		if err != nil && !errors.Is(err, context.Canceled) {
			oktetoLog.Warning("final log output will appear once this action is complete")
			oktetoLog.Infof("destroy preview logs cannot be streamed due to connectivity issues: %v", err)
		}
	}(&wg)

	select {
	case <-stop:
		ctxCancel()
		oktetoLog.Infof("CTRL+C received, exit")
		return oktetoErrors.ErrIntSig
	case err := <-exit:
		ctxCancel()
		// wait until streaming logs have finished
		wg.Wait()
		return err
	}
}

func (c destroyPreviewCommand) waitForPreviewDestroyed(ctx context.Context, preview string, timeout time.Duration) error {
	ticker := time.NewTicker(1 * time.Second)
	to := time.NewTicker(timeout)

	for {
		select {
		case <-to.C:
			return fmt.Errorf("%w: preview %s timed out after %s. Timeout can be increased using the '--timeout' flag", errDestroyPreviewTimeout, preview, timeout.String())
		case <-ticker.C:
			ns, err := c.k8sClient.CoreV1().Namespaces().Get(ctx, preview, metav1.GetOptions{})
			if err != nil {
				// not found error is expected when the namespace is deleted.
				// adding also IsForbidden as in some versions kubernetes returns this status when the namespace is deleted
				// one or the other we assume the namespace has been deleted
				if k8sErrors.IsNotFound(err) || k8sErrors.IsForbidden(err) {
					return nil
				}
				return err
			}

			status, ok := ns.Labels[constants.NamespaceStatusLabel]
			if !ok {
				// when status label is not present, continue polling the namespace until timeout
				oktetoLog.Debugf("namespace %q does not have label for status", preview)
				continue
			}
			// We should also check "Active" status. If a dev environment fails to be destroyed, we go back the preview status
			// to "Active" (okteto backend sets the status to deleting when starting the preview deletion)
			if status == "DeleteFailed" || status == "Active" {
				return errFailedDestroyPreview
			}
		}
	}
}
