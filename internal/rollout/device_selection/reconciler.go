package device_selection

import (
	"context"
	"net/http"

	api "github.com/flightctl/flightctl/api/v1alpha1"
	"github.com/flightctl/flightctl/internal/service"
	"github.com/flightctl/flightctl/internal/store"
	"github.com/flightctl/flightctl/internal/tasks_client"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/sirupsen/logrus"
)

type Reconciler interface {
	Reconcile(ctx context.Context)
}

const DeviceSelectionTaskName = "rollout-device-selection"

type reconciler struct {
	serviceHandler  service.Service
	log             logrus.FieldLogger
	callbackManager tasks_client.CallbackManager
}

func NewReconciler(serviceHandler service.Service, callbackManager tasks_client.CallbackManager, log logrus.FieldLogger) Reconciler {
	return &reconciler{
		serviceHandler:  serviceHandler,
		log:             log,
		callbackManager: callbackManager,
	}
}

func (r *reconciler) reconcileFleet(ctx context.Context, orgId uuid.UUID, fleet api.Fleet) {
	fleetName := lo.FromPtr(fleet.Metadata.Name)

	r.log.Infof("Device selection: starting reconciling fleet %v/%s", orgId, fleetName)
	defer r.log.Infof("Device selection: finished reconciling fleet %v/%s", orgId, fleetName)

	annotations := lo.FromPtr(fleet.Metadata.Annotations)
	if annotations == nil {
		r.log.Infof("Mo annotations for fleet %v/%s", orgId, fleetName)
		return
	}
	if fleet.Spec.RolloutPolicy == nil || fleet.Spec.RolloutPolicy.DeviceSelection == nil {
		r.log.Debugf("No device selection definition for fleet %v/%s", orgId, fleetName)
		rolloutWasActive, err := cleanupRollout(ctx, &fleet, r.serviceHandler)
		if err != nil {
			r.log.WithError(err).Errorf("%v/%s: CleanupRollout", orgId, fleetName)
		}
		if rolloutWasActive {

			// Send the entire fleet for rollout
			r.callbackManager.FleetRolloutSelectionUpdated(ctx, orgId, fleetName)
		}
		return
	}
	templateVersionName, exists := annotations[api.FleetAnnotationTemplateVersion]
	if !exists {
		r.log.Warnf("No template version for fleet %v/%s", orgId, fleetName)
		return
	}
	selector, err := NewRolloutDeviceSelector(fleet.Spec.RolloutPolicy.DeviceSelection, fleet.Spec.RolloutPolicy.DefaultUpdateTimeout, r.serviceHandler, orgId, &fleet, templateVersionName, r.log)
	if err != nil {
		r.log.WithError(err).Errorf("%v/%s: NewRolloutDeviceSelector", orgId, fleetName)
		return
	}
	definitionUpdated, err := selector.IsDefinitionUpdated()
	if err != nil {
		r.log.WithError(err).Errorf("%v/%s: IsDefinitionUpdated", orgId, fleetName)
		return
	}
	if selector.IsRolloutNew() || definitionUpdated {
		// There is either a new template version, or the rollout definition was updated
		if err = selector.OnNewRollout(ctx); err != nil {
			r.log.WithError(err).Errorf("%v/%s: OnNewRollout", orgId, fleetName)
			return
		}
		if err = selector.Reset(ctx); err != nil {
			r.log.WithError(err).Errorf("%v/%s: Reset", orgId, fleetName)
			return
		}
	}

	for {
		var selection Selection
		selection, err = selector.CurrentSelection(ctx)
		if err != nil {
			r.log.WithError(err).Errorf("%v/%s: CurrentSelection", orgId, fleetName)
			break
		}

		if !selection.IsApproved() {

			// A batch may be approved either by a user or automatically
			mayApprove, err := selection.MayApproveAutomatically()
			if err != nil {
				r.log.WithError(err).Errorf("%v/%s: MayApproveAutomatically", orgId, fleetName)
				break
			}
			if mayApprove {
				if err = selection.Approve(ctx); err != nil {
					r.log.WithError(err).Errorf("%v/%s: Approve", orgId, fleetName)
					break
				}
			} else {
				if err = selection.OnSuspended(ctx); err != nil {
					r.log.WithError(err).Errorf("%v/%s: OnSuspended", orgId, fleetName)
				}
				break
			}
		}

		// Check if all devices in the batch have been processed by the fleet-rollout task
		isRolledOut, err := selection.IsRolledOut(ctx)
		if err != nil {
			r.log.WithError(err).Errorf("%v/%s: IsRolledOut", orgId, fleetName)
			break
		}
		if !isRolledOut {
			if err = selection.OnRollout(ctx); err != nil {
				r.log.WithError(err).Errorf("%v/%s: OnRollout", orgId, fleetName)
			}
			// Send the current batch to be rolled out.
			r.callbackManager.FleetRolloutSelectionUpdated(ctx, orgId, fleetName)
		}

		// Is the current batch complete
		isComplete, err := selection.IsComplete(ctx)
		if err != nil {
			r.log.WithError(err).Errorf("%v/%s: IsComplete", orgId, fleetName)
			break
		}
		if !isComplete {
			break
		}

		// Once the batch is complete, set the success percentage of the current batch
		if err = selection.SetCompletionReport(ctx); err != nil {
			r.log.WithError(err).Errorf("%v/%s: SetCompletionReport", orgId, fleetName)
			break
		}
		hasMoreSelections, err := selector.HasMoreSelections(ctx)
		if err != nil {
			r.log.WithError(err).Errorf("%v/%s: HasMoreSelections", orgId, fleetName)
			break
		}
		if !hasMoreSelections {
			if err = selection.OnFinish(ctx); err != nil {
				r.log.WithError(err).Errorf("%v/%s: OnFinish", orgId, fleetName)
			}
			break
		}

		// Proceed to the next batch
		if err = selector.Advance(ctx); err != nil {
			r.log.WithError(err).Errorf("%v/%s: Advance", orgId, fleetName)
			break
		}
	}
}

func (r *reconciler) Reconcile(ctx context.Context) {
	// Get all relevant fleets
	orgId := store.NullOrgId

	fleetList, status := r.serviceHandler.ListFleetRolloutDeviceSelection(ctx)
	if status.Code != http.StatusOK {
		r.log.WithError(service.ApiStatusToErr(status)).Error("ListRolloutDeviceSelection")
		return
	}
	for _, fleet := range fleetList.Items {
		r.reconcileFleet(ctx, orgId, fleet)
	}
}
