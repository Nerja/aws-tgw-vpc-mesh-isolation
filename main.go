package main

import (
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2transitgateway"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func createVpc(ctx *pulumi.Context, name string, cidrBlock string) (*ec2.Vpc, *ec2.Subnet, error) {
	vpc, err := ec2.NewVpc(ctx, name, &ec2.VpcArgs{
		CidrBlock:          pulumi.String(cidrBlock),
		EnableDnsHostnames: pulumi.BoolPtr(true),
		EnableDnsSupport:   pulumi.BoolPtr(true),
	})
	if err != nil {
		return nil, nil, err
	}
	subnet, err := ec2.NewSubnet(ctx, fmt.Sprintf("%s-subnet", name), &ec2.SubnetArgs{
		CidrBlock: pulumi.String(cidrBlock),
		VpcId:     vpc.ID(),
	})
	if err != nil {
		return nil, nil, err
	}
	sg, err := ec2.NewSecurityGroup(ctx, fmt.Sprintf("vpc-%s-sg", name), &ec2.SecurityGroupArgs{
		Ingress: ec2.SecurityGroupIngressArray{
			&ec2.SecurityGroupIngressArgs{
				Description: pulumi.String("traffic from VPCs"),
				FromPort:    pulumi.Int(0),
				ToPort:      pulumi.Int(0),
				Protocol:    pulumi.String("-1"),
				CidrBlocks: pulumi.StringArray{
					pulumi.String("0.0.0.0/0"), // Only for demo purposes
				},
			},
		},
		Egress: ec2.SecurityGroupEgressArray{
			&ec2.SecurityGroupEgressArgs{
				FromPort: pulumi.Int(0),
				ToPort:   pulumi.Int(0),
				Protocol: pulumi.String("-1"),
				CidrBlocks: pulumi.StringArray{
					pulumi.String("0.0.0.0/0"), // Only for demo purposes
				},
			},
		},
		VpcId: vpc.ID(),
	})
	if err != nil {
		return nil, nil, err
	}
	ssmEndpoint, err := ec2.NewVpcEndpoint(ctx, fmt.Sprintf("vpc-%s", name), &ec2.VpcEndpointArgs{
		PrivateDnsEnabled: pulumi.BoolPtr(true),
		ServiceName:       pulumi.String("com.amazonaws.eu-north-1.ssm"),
		VpcEndpointType:   pulumi.String("Interface"),
		SubnetIds: pulumi.StringArray{
			subnet.ID(),
		},
		SecurityGroupIds: pulumi.StringArray{
			sg.ID(),
		},
		VpcId: vpc.ID(),
	})
	if err != nil {
		return nil, nil, err
	}
	ec2MessagesEndpoint, err := ec2.NewVpcEndpoint(ctx, fmt.Sprintf("vpc-%s-ec2messages", name), &ec2.VpcEndpointArgs{
		PrivateDnsEnabled: pulumi.BoolPtr(true),
		ServiceName:       pulumi.String("com.amazonaws.eu-north-1.ec2messages"),
		VpcEndpointType:   pulumi.String("Interface"),
		SubnetIds: pulumi.StringArray{
			subnet.ID(),
		},
		SecurityGroupIds: pulumi.StringArray{
			sg.ID(),
		},
		VpcId: vpc.ID(),
	})
	if err != nil {
		return nil, nil, err
	}
	ssmMessagesEndpoint, err := ec2.NewVpcEndpoint(ctx, fmt.Sprintf("vpc-%s-ssmmessages", name), &ec2.VpcEndpointArgs{
		PrivateDnsEnabled: pulumi.BoolPtr(true),
		ServiceName:       pulumi.String("com.amazonaws.eu-north-1.ssmmessages"),
		VpcEndpointType:   pulumi.String("Interface"),
		SubnetIds: pulumi.StringArray{
			subnet.ID(),
		},
		SecurityGroupIds: pulumi.StringArray{
			sg.ID(),
		},
		VpcId: vpc.ID(),
	})
	if err != nil {
		return nil, nil, err
	}

	_, err = ec2.NewInstance(ctx, fmt.Sprintf("vpc-%s-EC2", name), &ec2.InstanceArgs{
		Ami:      pulumi.String("ami-0b5483e9d9802be1f"),
		SubnetId: subnet.ID(),
		VpcSecurityGroupIds: pulumi.StringArray{
			sg.ID(),
		},
		InstanceType:       pulumi.String("t4g.nano"),
		IamInstanceProfile: pulumi.String("ec2-ssm-mgmt"),
	}, pulumi.DependsOn([]pulumi.Resource{ssmEndpoint, ssmMessagesEndpoint, ec2MessagesEndpoint}))
	if err != nil {
		return nil, nil, err
	}

	return vpc, subnet, nil
}

func attachVPCToTgw(ctx *pulumi.Context, name string, tgw *ec2transitgateway.TransitGateway, vpc *ec2.Vpc, subnet *ec2.Subnet, rtAssoc *ec2transitgateway.RouteTable, rtPropagations ...*ec2transitgateway.RouteTable) error {
	attachment, err := ec2transitgateway.NewVpcAttachment(ctx, fmt.Sprintf("tgw-attachment-%s", name), &ec2transitgateway.VpcAttachmentArgs{
		SubnetIds: pulumi.StringArray{
			subnet.ID(),
		},
		TransitGatewayId: tgw.ID(),
		VpcId:            vpc.ID(),
		Tags: pulumi.StringMap{
			"Name": pulumi.String(fmt.Sprintf("tgw-attachment-%s", name)),
		},
	})
	if err != nil {
		return err
	}

	// Set route table association for the attachment
	_, err = ec2transitgateway.NewRouteTableAssociation(ctx, fmt.Sprintf("tgw-attachment-rt-assoc-%s", name), &ec2transitgateway.RouteTableAssociationArgs{
		TransitGatewayAttachmentId: attachment.ID(),
		TransitGatewayRouteTableId: rtAssoc.ID(),
		ReplaceExistingAssociation: pulumi.Bool(true),
	})
	if err != nil {
		return err
	}

	// Propagate routes from the attachment to provided route tables
	for i, rt := range rtPropagations {
		_, err = ec2transitgateway.NewRouteTablePropagation(ctx, fmt.Sprintf("tgw-attachment-rt-prop-%s-%d", name, i), &ec2transitgateway.RouteTablePropagationArgs{
			TransitGatewayAttachmentId: attachment.ID(),
			TransitGatewayRouteTableId: rt.ID(),
		})
		if err != nil {
			return err
		}
	}
	// Propagate routes from the attachment as well
	_, err = ec2transitgateway.NewRouteTablePropagation(ctx, fmt.Sprintf("tgw-attachment-rt-prop-%s", name), &ec2transitgateway.RouteTablePropagationArgs{
		TransitGatewayAttachmentId: attachment.ID(),
		TransitGatewayRouteTableId: rtAssoc.ID(),
	})
	return err
}

func createSubnetRT(ctx *pulumi.Context, name string, vpc *ec2.Vpc, subnet *ec2.Subnet, tgw *ec2transitgateway.TransitGateway) error {
	rt, err := ec2.NewRouteTable(ctx, fmt.Sprintf("subnet-rt-%s", name), &ec2.RouteTableArgs{
		VpcId: vpc.ID(),
		Routes: ec2.RouteTableRouteArray{
			&ec2.RouteTableRouteArgs{
				CidrBlock:        pulumi.String("0.0.0.0/0"),
				TransitGatewayId: tgw.ID(),
			},
		},
	})
	if err != nil {
		return err
	}
	if _, err := ec2.NewRouteTableAssociation(ctx, fmt.Sprintf("subnet-rt-assoc-%s", name), &ec2.RouteTableAssociationArgs{
		SubnetId:     subnet.ID(),
		RouteTableId: rt.ID(),
	}); err != nil {
		return err
	}
	return nil
}

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		vpcA, subnetA, err := createVpc(ctx, "vpc-a", "10.0.0.0/24")
		if err != nil {
			return err
		}
		vpcB, subnetB, err := createVpc(ctx, "vpc-b", "10.0.1.0/24")
		if err != nil {
			return err
		}
		vpcC, subnetC, err := createVpc(ctx, "vpc-c", "10.0.2.0/24")
		if err != nil {
			return err
		}
		tgw, err := ec2transitgateway.NewTransitGateway(ctx, "tgw", &ec2transitgateway.TransitGatewayArgs{})
		if err != nil {
			return err
		}

		/* Create two groups that can communicate with each other.
		 * A can communicate with C, but B cannot communicate with A.
		 * B can communicate with C, but A cannot communicate with B.
		 * C can communicate with A and B. */

		tgwRTA, err := ec2transitgateway.NewRouteTable(ctx, "tgw-rt-a", &ec2transitgateway.RouteTableArgs{
			TransitGatewayId: tgw.ID(),
		})
		if err != nil {
			return err
		}

		tgwRTB, err := ec2transitgateway.NewRouteTable(ctx, "tgw-rt-b", &ec2transitgateway.RouteTableArgs{
			TransitGatewayId: tgw.ID(),
		})
		if err != nil {
			return err
		}

		tgwRTC, err := ec2transitgateway.NewRouteTable(ctx, "tgw-rt-c", &ec2transitgateway.RouteTableArgs{
			TransitGatewayId: tgw.ID(),
		})
		if err != nil {
			return err
		}

		if err := attachVPCToTgw(ctx, "A", tgw, vpcA, subnetA, tgwRTA, tgwRTC); err != nil {
			return err
		}
		if err := attachVPCToTgw(ctx, "B", tgw, vpcB, subnetB, tgwRTB, tgwRTC); err != nil {
			return err
		}
		if err := attachVPCToTgw(ctx, "C", tgw, vpcC, subnetC, tgwRTC, tgwRTA, tgwRTB); err != nil {
			return err
		}

		if err := createSubnetRT(ctx, "A", vpcA, subnetA, tgw); err != nil {
			return err
		}
		if err := createSubnetRT(ctx, "B", vpcB, subnetB, tgw); err != nil {
			return err
		}
		if err := createSubnetRT(ctx, "C", vpcC, subnetC, tgw); err != nil {
			return err
		}

		return nil
	})
}
