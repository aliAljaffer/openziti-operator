package desired

import zitiv1 "github.com/aliAljaffer/openziti-operator/api/v1alpha1"

func clusterOf(conn *zitiv1.ZitiConnection) string {
	if conn.Spec.ClusterID == "" {
		return "default"
	}
	return conn.Spec.ClusterID
}
