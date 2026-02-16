package provisioner

import (
	"encoding/base64"
	"fmt"
	"strings"

	core "github.com/nicol/dynamic-route-provisioner/core"
	provnetscaler "github.com/nicol/dynamic-route-provisioner/provisioner-netscaler"
)

// Compile-time check.
var _ provnetscaler.ResourceMapper = (*NetscalerMapper)(nil)

// NetscalerMapper translates RouteRequests into Nitro API operations that
// create an SSL lbvserver with backend services on a Netscaler CPX.
type NetscalerMapper struct{}

// RouteID derives a stable Nitro resource name from the route host.
// Dots are replaced with dashes to comply with Netscaler naming rules.
func (m *NetscalerMapper) RouteID(req core.RouteRequest) string {
	return "routes-" + strings.ReplaceAll(req.Host, ".", "-")
}

// MapProvision returns the ordered Nitro operations to create an SSL route:
// 1. Upload cert + key files
// 2. Create sslcertkey
// 3. Create SSL lbvserver
// 4. Add backend services
// 5. Bind services to vserver
// 6. Bind SSL cert to vserver
func (m *NetscalerMapper) MapProvision(req core.RouteRequest, cert *core.Certificate) ([]provnetscaler.NitroOperation, error) {
	id := m.RouteID(req)
	var ops []provnetscaler.NitroOperation

	// 1. Upload certificate and key as system files.
	if cert != nil {
		certFileName := id + ".crt"
		keyFileName := id + ".key"

		ops = append(ops,
			provnetscaler.NitroOperation{
				Action: provnetscaler.ActionAdd,
				Resource: provnetscaler.NitroResource{
					Type: "systemfile",
					Name: certFileName,
					Properties: map[string]any{
						"filename":     certFileName,
						"filelocation": "/nsconfig/ssl",
						"filecontent":  base64.StdEncoding.EncodeToString(cert.CertPEM),
						"fileencoding": "BASE64",
					},
				},
			},
			provnetscaler.NitroOperation{
				Action: provnetscaler.ActionAdd,
				Resource: provnetscaler.NitroResource{
					Type: "systemfile",
					Name: keyFileName,
					Properties: map[string]any{
						"filename":     keyFileName,
						"filelocation": "/nsconfig/ssl",
						"filecontent":  base64.StdEncoding.EncodeToString(cert.KeyPEM),
						"fileencoding": "BASE64",
					},
				},
			},
		)

		// 2. Create sslcertkey.
		ops = append(ops, provnetscaler.NitroOperation{
			Action: provnetscaler.ActionAdd,
			Resource: provnetscaler.NitroResource{
				Type: "sslcertkey",
				Name: id + "-cert",
				Properties: map[string]any{
					"cert": "/nsconfig/ssl/" + certFileName,
					"key":  "/nsconfig/ssl/" + keyFileName,
				},
			},
		})
	}

	// 3. Create SSL lbvserver.
	ops = append(ops, provnetscaler.NitroOperation{
		Action: provnetscaler.ActionAdd,
		Resource: provnetscaler.NitroResource{
			Type: "lbvserver",
			Name: id,
			Properties: map[string]any{
				"servicetype": "SSL",
				"ipv46":       "0.0.0.0",
				"port":        443,
				"lbmethod":    "ROUNDROBIN",
			},
		},
	})

	// 4. Add backend services and bind them.
	for i, backend := range req.Backends {
		svcName := fmt.Sprintf("%s-svc-%d", id, i)

		ops = append(ops,
			// Add the service.
			provnetscaler.NitroOperation{
				Action: provnetscaler.ActionAdd,
				Resource: provnetscaler.NitroResource{
					Type: "service",
					Name: svcName,
					Properties: map[string]any{
						"servername":  backend.ServiceName,
						"port":        backend.Port,
						"servicetype": "HTTP",
					},
				},
			},
			// Bind service to lbvserver.
			provnetscaler.NitroOperation{
				Action: provnetscaler.ActionBind,
				Resource: provnetscaler.NitroResource{
					Type: "lbvserver_service_binding",
					Name: id,
					Properties: map[string]any{
						"servicename": svcName,
						"weight":      backend.Weight,
					},
				},
			},
		)
	}

	// 5. Bind SSL cert to vserver.
	if cert != nil {
		ops = append(ops, provnetscaler.NitroOperation{
			Action: provnetscaler.ActionBind,
			Resource: provnetscaler.NitroResource{
				Type: "sslvserver_sslcertkey_binding",
				Name: id,
				Properties: map[string]any{
					"certkeyname": id + "-cert",
				},
			},
		})
	}

	return ops, nil
}

// MapDeprovision returns the ordered Nitro operations to tear down a route:
// unbind SSL cert, delete lbvserver, delete sslcertkey, delete cert/key files.
func (m *NetscalerMapper) MapDeprovision(routeID string) ([]provnetscaler.NitroOperation, error) {
	return []provnetscaler.NitroOperation{
		// Unbind SSL cert from vserver.
		{
			Action: provnetscaler.ActionUnbind,
			Resource: provnetscaler.NitroResource{
				Type: "sslvserver_sslcertkey_binding",
				Name: routeID,
				Properties: map[string]any{
					"certkeyname": routeID + "-cert",
				},
			},
		},
		// Delete lbvserver (also removes service bindings).
		{
			Action: provnetscaler.ActionDelete,
			Resource: provnetscaler.NitroResource{
				Type: "lbvserver",
				Name: routeID,
			},
		},
		// Delete sslcertkey.
		{
			Action: provnetscaler.ActionDelete,
			Resource: provnetscaler.NitroResource{
				Type: "sslcertkey",
				Name: routeID + "-cert",
			},
		},
		// Delete cert and key files.
		{
			Action: provnetscaler.ActionDelete,
			Resource: provnetscaler.NitroResource{
				Type: "systemfile",
				Name: routeID + ".crt",
				Properties: map[string]any{
					"filelocation": "/nsconfig/ssl",
				},
			},
		},
		{
			Action: provnetscaler.ActionDelete,
			Resource: provnetscaler.NitroResource{
				Type: "systemfile",
				Name: routeID + ".key",
				Properties: map[string]any{
					"filelocation": "/nsconfig/ssl",
				},
			},
		},
	}, nil
}

// MapList returns the resource type and name prefix for discovering routes.
func (m *NetscalerMapper) MapList() (string, string) {
	return "lbvserver", "routes-"
}

// MapBatchProvision combines provision operations for multiple routes.
func (m *NetscalerMapper) MapBatchProvision(routes []core.RouteRequest, certs map[string]*core.Certificate) ([]provnetscaler.NitroOperation, error) {
	var allOps []provnetscaler.NitroOperation
	for _, route := range routes {
		cert := certs[route.Host]
		ops, err := m.MapProvision(route, cert)
		if err != nil {
			return nil, err
		}
		allOps = append(allOps, ops...)
	}
	return allOps, nil
}

// MapBatchDeprovision combines deprovision operations for multiple routes.
func (m *NetscalerMapper) MapBatchDeprovision(routeIDs []string) ([]provnetscaler.NitroOperation, error) {
	var allOps []provnetscaler.NitroOperation
	for _, id := range routeIDs {
		ops, err := m.MapDeprovision(id)
		if err != nil {
			return nil, err
		}
		allOps = append(allOps, ops...)
	}
	return allOps, nil
}
