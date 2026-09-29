# Uninstall

## Keep the Ziti entities

Choose one way.

- Create the resource with `deletionPolicy: Orphan`. The setting cannot change later. When you delete the resource, the operator removes its own tags and the entities stay in Ziti as unmanaged entities.
- For a resource that already exists, change `managementPolicy` to `Observe`, then delete it. The operator does not touch Ziti. The entities keep their ownership tags, so the orphan sweeper reports them.

## Remove everything

1. Delete the custom resources first. The default `deletionPolicy: Delete` removes the Ziti entities.

   ```sh
   kubectl delete ztapp,ztid,ztap --all --all-namespaces
   ```

2. Delete the connection last.

   ```sh
   kubectl delete ztconn --all
   ```

3. Remove the operator and the CRDs.

   ```sh
   kubectl delete -f dist/install.yaml
   ```

## Warning

Delete the `ZitiConnection` after the resources. A resource cannot finish deleting without its connection, because the operator needs it to reach Ziti. If the connection is already gone, set the resource to `Orphan` or remove its finalizer.
