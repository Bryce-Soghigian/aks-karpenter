# One Pager: BYO Subnet Per AKSNodeClass 
This document outlines the behaviors and challenges we need to solve for to comfortably support BYO Subnet as a field in the AKSNodeClass. 
These are the high level ideas that will be covered in the document

1. API Design for representing SubnetIDs for an AKSNodeClass. 
2. AKSNodeClass SubnetHealth status conditions
3. Extending our E2E Framework to allow for testing of all the required networking scenarios

# API Design
## Approach A: Single Subnet Per AKSNodeClass 
The idea here, is that for each Nodepool, we have a single subnet. This supports the existing AKS patterns, is well tested and thoroughly used. It may not make as much sense in a karpenter world. Unlike the traditional AKS Nodepools, karpenter nodepools may have a much larger range of instance types and less isolation. 

### API 
```yaml
apiVersion: karpenter.azure.com/v1alpha2
kind: AKSNodeClass
metadata:
  name: system-surge
  annotations:
    kubernetes.io/description: "General purpose AKSNodeClass for running Ubuntu2204 nodes"
spec:
  imageFamily: Ubuntu2204
  subnet: "subnet1"
```

Let talk about the following 
1. Should subnets be immutable? 
2. How does validation work if the subnet is immutable? 

### Should VnetSubnetID be immutable?  
In AKS, we do not allow mutation of the Agentpool's --vnet-subnet-id. The way it works in AKS is subnets are immutable.  

In the case of actually starting to specify the vnetSubnetID we resolved from the nodeclass, what happens to the nodes?  

For example, lets say I create an AKS cluster, and specify a custom vnet + subnet id on cluster create.
az aks create --vnet-subnet-id "/vnet/silly/subnet/goose1". We pass this subnet id down to karpenter, and all of the karpenter nodes without a subnet id specified in the nodeclass are getting this as their default subnet. I do a scale up and I create 10 karpenter nodes. All of the nics for these nodes have their subnet set as "goose1".

What happens then when I take nodes that belong to "general-purpose" nodeclass, that have been getting this "goose1" subnet, and I specify my own subnet? 
When I apply a AKSNodeClass/Patch to the apiserver, specifying a new subnet "/vnet/silly/subnet/goose2", what do we do with the old nodes? If we don't support subnet drift, do they just stay on the old subnet? That can't be right. 

So if we support specifying VNETSubnetID in the nodeclass, we need to be supporting drift(mutable subnet ids), or alternatively, the AKSNodeClass can have the Vnet Subnet ID be required to be specified on the AKSNodeClass. 

Lets contrast the user experience of having drift, vs having the SubnetID be required.

#### How would we support drift for a single subnet id?   
Karpenter has a mechanism called drift, where we can mark the subnet as drifted on the nodeclaim, and karpenter will go and replace that node when it can. The aws provider also has a subnet drift state.
To see if a subnet has drifted we need to see does the expectedSubnet != actualSubnet. 
Expected subnet can be defined as either --vnet-subnet-id flag from global level, or the VnetSubnetID specifed on the nodeclass.
Actual Subnet is defined as the subnet on the network interface. 

For drift this then begs the question of how should we fix that drift from desired state? Should we mark the node as drifted and delete the nodeclaim for it to come back? Or can we modify the network interface in place?  

#### What would the experience look like to users if we require subnet id on the AKSNodeClass? 
Simply you would have to specify a subnet from the beginning. 


##### How Does Defaulting work for the existing nodeclasses AKS provides? 
Node Auto Provisioning requires aks gives two default nodeclasses. They are known as the `system-surge` nodeclass and the `general-purpose` nodeclass. If we require the subnet id be specifed on the nodeclass, would we simply propagate the default subnet id when creating the nodeclasses to use the one specified on cluster create? If we make it required and immutable, this means that the NodeClasses AKS forces onto the user will always exist as well as that subnet they specified. 

Users can delete them, and they reconcile back, but after they delete the nodeclass + nodepool the nodes would be rolled. I believe this is an acceptable user experience, and if users do not want these nodepools, they can simply specify a weight on another nodepool making it so they dont get nodes from the default nodepools.(This raises other challenges for example the user can effectively break the system-surge pool autoscaling by doing this)


#### Validation 
In the case of runtime validation for the vnet subnet id, we can set the nodeclass as not ready if we see that the subnet id specifed was invalid. Then the user has to delete and reapply the AKSNodeClass to reapply a valid subnet. 

Karpneters runtime validation will set the AKSNodeClass as ready when we are certain the Subnet is Available. We can raise telemetry on the nodeclass in the msg that they need to recreate with a valid subnet id if we see the subnet is not ready. The nodeclass should start as not ready until 


#### NodeClass Status Conditions 
```yaml
apiVersion: karpenter.azure.com/v1alpha2
kind: AKSNodeClass
metadata:
  name: system-surge
  annotations:
    kubernetes.io/description: "General purpose AKSNodeClass for running Ubuntu2204 nodes"
spec:
  imageFamily: Ubuntu2204

status: 
    - Ready: "True || False || Unknown"
      Reason: "SubnetFull || SubnetUnavailable || MissingVnetRBAC"
      Message: "Longer Error message describing the errors and potential mitigation"
```
The status condition here will halt autoscaling for a given nodepool if any of these conditions are met and its nodeclass is unhealthy.

##### SubnetUnavailable 
If the subnet specifed on the nodeclass is invalid, or unreachable/not found in arm, we mark this nodeclass as unavailable. If the customer deletes their subnet under the hood, or we cannot access it, we should be marking it as unavailable. 


##### SubnetFull
A typical failure case for cluster autoscaler is the vmss is attached to a subnet, and that subnet is full. Karpenter attaches individual vms through their network interface to a given subnet. 

The customers will handle subnet full by creating a new nodepool, with a new subnet. This is the recommended mitigation today on AKS. One thing karpenter can do is raise a status condition "NodeclassHealthy when the subnet is full. This will then exclude this nodepool attached to this nodeclass from being considered in autoscaling. 

##### MissingVNETRBAC 
Rather than us crashing when we do not see the right rbac for karpenter, we should wait until we have access to the vnet. If we see an authorization error for the vnet, we should be raising the appropriate status condition somewhere.  Karpenter should make it clear in the events and the status conditions that the user needs to assign the proper vnet/read, subnets/join, and subnets/read roles to the cluster identity for us to be able to autoscale.

### Approach B: Multiple Subnets Per AKSNodeClass
Karpenter follows a new model for nodepools where one Nodepool can represent all of the nodes in a cluster.  Traditional AKS patterns of having a single subnet may not make total sense. 

AWS Follows a pattern via the SubnetSelectorTerms. We could adopt a similar model. 

API A: 
```yaml
apiVersion: karpenter.azure.com/v1alpha2
kind: AKSNodeClass
metadata:
  name: system-surge
  annotations:
    kubernetes.io/description: "General purpose AKSNodeClass for running Ubuntu2204 nodes"
spec:
  imageFamily: Ubuntu2204
  subnets: [
    "subnet1",
    "anotherSubnet"
    ]
```
API B:
```yaml
kind: AKSNodeClass
spec:
  imageFamily: Ubuntu2204
  # Required, discovers subnets to attach to instances
  # Each term in the array of subnetSelectorTerms is ORed together
  # Within a single term, all conditions are ANDed
  subnetSelectorTerms:
    # Select on any subnet that has the "karpenter.sh/discovery: ${CLUSTER_NAME}"
    # AND the "environment: test" tag OR any subnet with ID "subnet1" in the vnet
    - tags:
        karpenter.sh/discovery: "${CLUSTER_NAME}"
        environment: test
    - id: 
      subnet1"
```

This API Surface area allows the user to specify multiple subnets directly that they have allocated that this nodeclass can consider. Karpenter can allocate nodes to whichever subnet has more available IP addresses at the moment of allocation.

### Q: Is there a world where the tag based discovery from the nodeclass makes sense to us? 
A large consideration when choosing to use API A or API B in our multiple subnet model is does the tags based selection make sense for AKS Architectures? Are there any 

### Approach 2.B Subnet Selector Terms 



# NodeClass Status Conditions 
```yaml
apiVersion: karpenter.azure.com/v1alpha2
kind: AKSNodeClass
metadata:
  name: system-surge
  annotations:
    kubernetes.io/description: "General purpose AKSNodeClass for running Ubuntu2204 nodes"
spec:
  imageFamily: Ubuntu2204

status: 
  Ready: "True || False || Unknown"
  Reason: "SubnetFull || SubnetUnavailable || MissingVnetRBAC"
  Message: "Longer Error message describing the errors"
```
The status condition here will halt autoscaling for a given nodepool if any of these conditions are met and its nodeclass is unhealthy.

## Single Subnet Per NodeClass: AKSNodeClass Ready
A typical failure case for cluster autoscaler is the vmss is attached to a subnet, and that subnet is full. Karpenter attaches individual vms through their network interface to a given subnet. 

The customers will handle subnet full by creating a new nodepool, with a new subnet. This is the recommended mitigation today on AKS. One thing karpenter can do is raise a status condition "NodeclassHealthy when the subnet is full. This will then exclude this nodepool attached to this nodeclass from being considered in autoscaling. 

## MultiSubnet Per NodeClass:  AKSNodeClass Ready
If there is a single subnet that is "Ready"(Not full and available), we mark the nodeclass as ready as it can support provisioning. I like this model because the whole idea from the AKS motivation to have multiple subnets in the nodeclass is to have fallbacks in case of the "SubnetFull" error cases. So in that sense we are still healthy so long as we can keep connecting nics via a join to the subnet.

# Evolving the Testing Framework
Today the karpenter testing framework runs all of our e2es on a single cluster configuration. 

Azure CNI + Overlay + Cilium with managed vnet + subnet. To test varous networking features, our testing framework needs to evolve. In particular we need to add azure clients for generating cluster configurations to run the tests on, and generate other azure resources(ACR Images, Subnets). 


# Questions && Discussion Points
**Q:** In AKS Clusters today, can I change the --vnet-subnet-id set on my agentpool? If so, what is the process for moving the existing nodes into the new subnet?
**A:** No, the field is immutable today and would be considered a large feature to make it mutable.
**Q:** What patterns would emerge that make good use of tags for subnet discovery? 
## Q: How should subnet IDs be specified? 
AKS only allows our users to specify a single vnet. Ideally as a user I wouldn't have to include the entire ARM ID, as we require in AKS. Instead I can just specify the subnetname. Then karpenter can reconstruct these ids based on the info it has from the original vnet id.


Rather than 
```yaml
apiVersion: karpenter.azure.com/v1alpha2
kind: AKSNodeClass
metadata:
  name: system-surge
  annotations:
    kubernetes.io/description: "General purpose AKSNodeClass for running Ubuntu2204 nodes"
spec:
  imageFamily: Ubuntu2204
  subnets: [
    "/subscriptions/12345678-1234-1234-1234-123456789012/resourceGroups/rgname/providers/Microsoft.Network/virtualNetworks/same-vnet/subnets/subnet1",
    "/subscriptions/12345678-1234-1234-1234-123456789012/resourceGroups/rgname/providers/Microsoft.Network/virtualNetworks/same-vnet/subnets/anotherSubnet"
    ]
```
We can do 

```yaml
apiVersion: karpenter.azure.com/v1alpha2
kind: AKSNodeClass
metadata:
  name: system-surge
  annotations:
    kubernetes.io/description: "General purpose AKSNodeClass for running Ubuntu2204 nodes"
spec:
  imageFamily: Ubuntu2204
  subnets: [
    "subnet1",
    "anotherSubnet",
  ]
```
Since we know the VNET ID, we can resolve all required parameters. Traditionally AKS has required the full VNETSubnetID since you are also specifying which vnet you are using the cluster this way for BYO VNET + SUBNET. 

But for karpenter, if I am using custom subnet per nodeclass, the user must be using custom vnet for their cluster. I can't have a karpenter custom subnet without specifying an AKS --vnet-subnet-id.

So for our API Surface area, rather than a full ARM ID, we can simply do the subnet name.
Another benefit of this configuration, is it makes it more explict to the user that they need to do that initial aks update to configure the custom vnet. 

