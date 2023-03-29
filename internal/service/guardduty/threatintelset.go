package guardduty

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/arn"
	"github.com/aws/aws-sdk-go/service/guardduty"
	"github.com/hashicorp/aws-sdk-go-base/v2/awsv1shim/v2/tfawserr"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/retry"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"github.com/hashicorp/terraform-provider-aws/internal/conns"
	"github.com/hashicorp/terraform-provider-aws/internal/errs/sdkdiag"
	tftags "github.com/hashicorp/terraform-provider-aws/internal/tags"
	"github.com/hashicorp/terraform-provider-aws/internal/verify"
)

// @SDKResource("aws_guardduty_threatintelset")
func ResourceThreatIntelSet() *schema.Resource {
	return &schema.Resource{
		CreateWithoutTimeout: resourceThreatIntelSetCreate,
		ReadWithoutTimeout:   resourceThreatIntelSetRead,
		UpdateWithoutTimeout: resourceThreatIntelSetUpdate,
		DeleteWithoutTimeout: resourceThreatIntelSetDelete,

		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			"arn": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"detector_id": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"name": {
				Type:     schema.TypeString,
				Required: true,
			},
			"format": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
				ValidateFunc: validation.StringInSlice([]string{
					guardduty.ThreatIntelSetFormatTxt,
					guardduty.ThreatIntelSetFormatStix,
					guardduty.ThreatIntelSetFormatOtxCsv,
					guardduty.ThreatIntelSetFormatAlienVault,
					guardduty.ThreatIntelSetFormatProofPoint,
					guardduty.ThreatIntelSetFormatFireEye,
				}, false),
			},
			"location": {
				Type:     schema.TypeString,
				Required: true,
			},
			"activate": {
				Type:     schema.TypeBool,
				Required: true,
			},
			"tags": tftags.TagsSchema(),

			"tags_all": tftags.TagsSchemaComputed(),
		},

		CustomizeDiff: verify.SetTagsDiff,
	}
}

func resourceThreatIntelSetCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	conn := meta.(*conns.AWSClient).GuardDutyConn()
	defaultTagsConfig := meta.(*conns.AWSClient).DefaultTagsConfig
	tags := defaultTagsConfig.MergeTags(tftags.New(ctx, d.Get("tags").(map[string]interface{})))

	detectorID := d.Get("detector_id").(string)
	name := d.Get("name").(string)
	input := &guardduty.CreateThreatIntelSetInput{
		DetectorId: aws.String(detectorID),
		Name:       aws.String(name),
		Format:     aws.String(d.Get("format").(string)),
		Location:   aws.String(d.Get("location").(string)),
		Activate:   aws.Bool(d.Get("activate").(bool)),
	}

	if len(tags) > 0 {
		input.Tags = Tags(tags.IgnoreAWS())
	}

	resp, err := conn.CreateThreatIntelSetWithContext(ctx, input)
	if err != nil {
		return sdkdiag.AppendErrorf(diags, "creating GuardDuty Threat Intel Set (%s): %s", name, err)
	}

	stateConf := &retry.StateChangeConf{
		Pending:    []string{guardduty.ThreatIntelSetStatusActivating, guardduty.ThreatIntelSetStatusDeactivating},
		Target:     []string{guardduty.ThreatIntelSetStatusActive, guardduty.ThreatIntelSetStatusInactive},
		Refresh:    threatintelsetRefreshStatusFunc(ctx, conn, *resp.ThreatIntelSetId, detectorID),
		Timeout:    5 * time.Minute,
		MinTimeout: 3 * time.Second,
	}

	if _, err = stateConf.WaitForStateContext(ctx); err != nil {
		return sdkdiag.AppendErrorf(diags, "creating GuardDuty Threat Intel Set (%s): waiting for completion: %s", name, err)
	}

	d.SetId(fmt.Sprintf("%s:%s", detectorID, aws.StringValue(resp.ThreatIntelSetId)))

	return append(diags, resourceThreatIntelSetRead(ctx, d, meta)...)
}

func resourceThreatIntelSetRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	conn := meta.(*conns.AWSClient).GuardDutyConn()
	defaultTagsConfig := meta.(*conns.AWSClient).DefaultTagsConfig
	ignoreTagsConfig := meta.(*conns.AWSClient).IgnoreTagsConfig

	threatIntelSetId, detectorId, err := DecodeThreatIntelSetID(d.Id())
	if err != nil {
		return sdkdiag.AppendErrorf(diags, "reading GuardDuty Threat Intel Set (%s): %s", d.Id(), err)
	}
	input := &guardduty.GetThreatIntelSetInput{
		DetectorId:       aws.String(detectorId),
		ThreatIntelSetId: aws.String(threatIntelSetId),
	}

	resp, err := conn.GetThreatIntelSetWithContext(ctx, input)
	if err != nil {
		if tfawserr.ErrMessageContains(err, guardduty.ErrCodeBadRequestException, "The request is rejected because the input detectorId is not owned by the current account.") {
			log.Printf("[WARN] GuardDuty ThreatIntelSet %q not found, removing from state", threatIntelSetId)
			d.SetId("")
			return diags
		}
		return sdkdiag.AppendErrorf(diags, "reading GuardDuty Threat Intel Set (%s): %s", d.Id(), err)
	}

	arn := arn.ARN{
		Partition: meta.(*conns.AWSClient).Partition,
		Region:    meta.(*conns.AWSClient).Region,
		Service:   "guardduty",
		AccountID: meta.(*conns.AWSClient).AccountID,
		Resource:  fmt.Sprintf("detector/%s/threatintelset/%s", detectorId, threatIntelSetId),
	}.String()
	d.Set("arn", arn)

	d.Set("detector_id", detectorId)
	d.Set("format", resp.Format)
	d.Set("location", resp.Location)
	d.Set("name", resp.Name)
	d.Set("activate", aws.StringValue(resp.Status) == guardduty.ThreatIntelSetStatusActive)

	tags := KeyValueTags(ctx, resp.Tags).IgnoreAWS().IgnoreConfig(ignoreTagsConfig)

	//lintignore:AWSR002
	if err := d.Set("tags", tags.RemoveDefaultConfig(defaultTagsConfig).Map()); err != nil {
		return sdkdiag.AppendErrorf(diags, "setting tags: %s", err)
	}

	if err := d.Set("tags_all", tags.Map()); err != nil {
		return sdkdiag.AppendErrorf(diags, "setting tags_all: %s", err)
	}

	return diags
}

func resourceThreatIntelSetUpdate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	conn := meta.(*conns.AWSClient).GuardDutyConn()

	threatIntelSetID, detectorId, err := DecodeThreatIntelSetID(d.Id())
	if err != nil {
		return sdkdiag.AppendErrorf(diags, "updating GuardDuty Threat Intel Set (%s): %s", d.Id(), err)
	}

	if d.HasChanges("activate", "location", "name") {
		input := &guardduty.UpdateThreatIntelSetInput{
			DetectorId:       aws.String(detectorId),
			ThreatIntelSetId: aws.String(threatIntelSetID),
		}

		if d.HasChange("name") {
			input.Name = aws.String(d.Get("name").(string))
		}
		if d.HasChange("location") {
			input.Location = aws.String(d.Get("location").(string))
		}
		if d.HasChange("activate") {
			input.Activate = aws.Bool(d.Get("activate").(bool))
		}

		if _, err = conn.UpdateThreatIntelSetWithContext(ctx, input); err != nil {
			return sdkdiag.AppendErrorf(diags, "updating GuardDuty Threat Intel Set (%s): %s", d.Id(), err)
		}
	}

	if d.HasChange("tags_all") {
		o, n := d.GetChange("tags_all")

		if err := UpdateTags(ctx, conn, d.Get("arn").(string), o, n); err != nil {
			return sdkdiag.AppendErrorf(diags, "updating GuardDuty Threat Intel Set (%s): setting tags: %s", d.Id(), err)
		}
	}

	return append(diags, resourceThreatIntelSetRead(ctx, d, meta)...)
}

func resourceThreatIntelSetDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	conn := meta.(*conns.AWSClient).GuardDutyConn()

	threatIntelSetID, detectorId, err := DecodeThreatIntelSetID(d.Id())
	if err != nil {
		return sdkdiag.AppendErrorf(diags, "deleting GuardDuty Threat Intel Set (%s): %s", d.Id(), err)
	}
	input := &guardduty.DeleteThreatIntelSetInput{
		DetectorId:       aws.String(detectorId),
		ThreatIntelSetId: aws.String(threatIntelSetID),
	}

	_, err = conn.DeleteThreatIntelSetWithContext(ctx, input)
	if err != nil {
		return sdkdiag.AppendErrorf(diags, "deleting GuardDuty Threat Intel Set (%s): %s", d.Id(), err)
	}

	stateConf := &retry.StateChangeConf{
		Pending: []string{
			guardduty.ThreatIntelSetStatusActive,
			guardduty.ThreatIntelSetStatusActivating,
			guardduty.ThreatIntelSetStatusInactive,
			guardduty.ThreatIntelSetStatusDeactivating,
			guardduty.ThreatIntelSetStatusDeletePending,
		},
		Target:     []string{guardduty.ThreatIntelSetStatusDeleted},
		Refresh:    threatintelsetRefreshStatusFunc(ctx, conn, threatIntelSetID, detectorId),
		Timeout:    5 * time.Minute,
		MinTimeout: 3 * time.Second,
	}

	_, err = stateConf.WaitForStateContext(ctx)
	if err != nil {
		return sdkdiag.AppendErrorf(diags, "waiting for GuardDuty ThreatIntelSet status to be \"%s\": %s", guardduty.ThreatIntelSetStatusDeleted, err)
	}

	return diags
}

func threatintelsetRefreshStatusFunc(ctx context.Context, conn *guardduty.GuardDuty, threatIntelSetID, detectorID string) retry.StateRefreshFunc {
	return func() (interface{}, string, error) {
		input := &guardduty.GetThreatIntelSetInput{
			DetectorId:       aws.String(detectorID),
			ThreatIntelSetId: aws.String(threatIntelSetID),
		}
		resp, err := conn.GetThreatIntelSetWithContext(ctx, input)
		if err != nil {
			return nil, "failed", err
		}
		return resp, *resp.Status, nil
	}
}

func DecodeThreatIntelSetID(id string) (threatIntelSetID, detectorID string, err error) {
	parts := strings.Split(id, ":")
	if len(parts) != 2 {
		err = fmt.Errorf("GuardDuty ThreatIntelSet ID must be of the form <Detector ID>:<ThreatIntelSet ID>, was provided: %s", id)
		return
	}
	threatIntelSetID = parts[1]
	detectorID = parts[0]
	return
}
