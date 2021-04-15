package machinehandler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	ErrUnstructuredFieldNotFound = fmt.Errorf("field not found")
)

type MachineHandler struct {
	APIGroup string
	Client   client.Client
	Config   *rest.Config
	Ctx      context.Context
	machines []unstructured.Unstructured
}

// machineAddress contains information for the node's address.
type machineAddress struct {
	// Machine address type, one of Hostname, ExternalIP or InternalIP.
	Type string `json:"type"`

	// The machine address.
	Address string `json:"address"`
}

// MachineAddresses is a slice of MachineAddress items to be used by infrastructure providers.
type machineAddresses []machineAddress

func (m *MachineHandler) ListMachines() error {
	APIVersion, err := m.getAPIGroupPreferredVersion()
	if err != nil {
		return err
	}

	unstructuredMachineList := &unstructured.UnstructuredList{}
	unstructuredMachineList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   m.APIGroup,
		Kind:    "MachineList",
		Version: APIVersion,
	})
	if err := m.Client.List(m.Ctx, unstructuredMachineList); err != nil {
		return err
	}

	m.machines = unstructuredMachineList.Items

	return nil
}

// getAPIGroupPreferredVersion get preferred API version using API group
func (m *MachineHandler) getAPIGroupPreferredVersion() (string, error) {
	if m.Config == nil {
		return "", fmt.Errorf("machine handler config can't be nil")
	}

	managementDiscoveryClient, err := discovery.NewDiscoveryClientForConfig(m.Config)
	if err != nil {
		return "", fmt.Errorf("create discovery client failed: %v", err)
	}

	groupList, err := managementDiscoveryClient.ServerGroups()
	if err != nil {
		return "", fmt.Errorf("failed to get ServerGroups: %v", err)
	}

	for _, group := range groupList.Groups {
		if group.Name == m.APIGroup {
			return group.PreferredVersion.Version, nil
		}
	}

	return "", fmt.Errorf("failed to find API group %q", m.APIGroup)
}

func (m *MachineHandler) GetMachines() []unstructured.Unstructured {
	return m.machines
}

func (m *MachineHandler) FindMatchingMachineFromInternalDNS(nodeName string) (*unstructured.Unstructured, error) {
	for _, machine := range m.machines {

		machineAddresses, err := m.GetMachineAddresses(machine)
		if err != nil {
			return nil, err
		}

		for _, address := range machineAddresses {
			if corev1.NodeAddressType(address.Type) == corev1.NodeInternalDNS && address.Address == nodeName {
				return &machine, nil
			}
		}
	}
	return nil, fmt.Errorf("matching machine not found")
}

func (m *MachineHandler) FindMatchingMachineFromNodeRef(nodeName string) (*unstructured.Unstructured, error) {
	for _, machine := range m.machines {
		nodeRef, err := m.GetMachineNodeRef(&machine)
		if err != nil {
			return nil, err
		}

		if nodeRef != nil && nodeRef.Name == nodeName {
			return &machine, nil
		}

	}
	return nil, fmt.Errorf("matching machine not found")
}

func (m *MachineHandler) GetMachineNodeRef(machine *unstructured.Unstructured) (*corev1.ObjectReference, error) {
	var nodeRef *corev1.ObjectReference

	if err := unstructuredUnmarshalField(machine, &nodeRef, "status", "nodeRef"); err != nil {
		return nil, err
	}

	return nodeRef, nil
}

func (m *MachineHandler) GetMachineCreationTimestamp(machine *unstructured.Unstructured) (*metav1.Time, error) {
	var creationTimestamp *metav1.Time

	if err := unstructuredUnmarshalField(machine, &creationTimestamp, "metadata", "creationTimestamp"); err != nil {
		return nil, err
	}

	return creationTimestamp, nil
}

func (m *MachineHandler) GetMachineAddresses(machine unstructured.Unstructured) (machineAddresses, error) {
	machineAddresses := machineAddresses{}
	if err := unstructuredUnmarshalField(&machine, &machineAddresses, "status", "addresses"); err != nil {
		return machineAddresses, err
	}
	return machineAddresses, nil
}

// unstructuredUnmarshalField is a wrapper around json and unstructured objects to decode and copy a specific field
// value into an object.
func unstructuredUnmarshalField(obj *unstructured.Unstructured, v interface{}, fields ...string) error {
	value, found, err := unstructured.NestedFieldNoCopy(obj.Object, fields...)
	if err != nil {
		return fmt.Errorf("failed to retrieve field %q from %q: %v", strings.Join(fields, "."), obj.GroupVersionKind(), err)
	}
	if !found || value == nil {
		return ErrUnstructuredFieldNotFound
	}
	valueBytes, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to json-encode field %q value from %q: %v", strings.Join(fields, "."), obj.GroupVersionKind(), err)
	}
	if err := json.Unmarshal(valueBytes, v); err != nil {
		return fmt.Errorf("failed to json-decode field %q value from %q: %v", strings.Join(fields, "."), obj.GroupVersionKind(), err)
	}
	return nil
}
