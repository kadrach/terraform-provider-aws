package ssm

import (
	"context"
	"log"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ssm"
	"github.com/hashicorp/aws-sdk-go-base/v2/awsv1shim/v2/tfawserr"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/retry"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
	"github.com/hashicorp/terraform-provider-aws/internal/conns"
	"github.com/hashicorp/terraform-provider-aws/internal/errs/sdkdiag"
	tftags "github.com/hashicorp/terraform-provider-aws/internal/tags"
	"github.com/hashicorp/terraform-provider-aws/internal/tfresource"
	"github.com/hashicorp/terraform-provider-aws/internal/verify"
)

// @SDKResource("aws_ssm_activation")
func ResourceActivation() *schema.Resource {
	return &schema.Resource{
		CreateWithoutTimeout: resourceActivationCreate,
		ReadWithoutTimeout:   resourceActivationRead,
		DeleteWithoutTimeout: resourceActivationDelete,
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},

		Schema: map[string]*schema.Schema{
			"name": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
			},
			"description": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
			},
			"expired": {
				Type:     schema.TypeBool,
				Computed: true,
			},
			"expiration_date": {
				Type:         schema.TypeString,
				Optional:     true,
				Computed:     true,
				ForceNew:     true,
				ValidateFunc: validation.IsRFC3339Time,
			},
			"iam_role": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"registration_limit": {
				Type:     schema.TypeInt,
				Optional: true,
				ForceNew: true,
			},
			"registration_count": {
				Type:     schema.TypeInt,
				Computed: true,
			},
			"activation_code": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"tags":     tftags.TagsSchemaForceNew(),
			"tags_all": tftags.TagsSchemaComputed(),
		},

		CustomizeDiff: verify.SetTagsDiff,
	}
}

func resourceActivationCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	conn := meta.(*conns.AWSClient).SSMConn()
	defaultTagsConfig := meta.(*conns.AWSClient).DefaultTagsConfig
	tags := defaultTagsConfig.MergeTags(tftags.New(ctx, d.Get("tags").(map[string]interface{})))

	log.Printf("[DEBUG] SSM activation create: %s", d.Id())

	activationInput := &ssm.CreateActivationInput{
		IamRole: aws.String(d.Get("name").(string)),
	}

	if _, ok := d.GetOk("name"); ok {
		activationInput.DefaultInstanceName = aws.String(d.Get("name").(string))
	}

	if _, ok := d.GetOk("description"); ok {
		activationInput.Description = aws.String(d.Get("description").(string))
	}

	if v, ok := d.GetOk("expiration_date"); ok {
		t, _ := time.Parse(time.RFC3339, v.(string))
		activationInput.ExpirationDate = aws.Time(t)
	}

	if _, ok := d.GetOk("iam_role"); ok {
		activationInput.IamRole = aws.String(d.Get("iam_role").(string))
	}

	if _, ok := d.GetOk("registration_limit"); ok {
		activationInput.RegistrationLimit = aws.Int64(int64(d.Get("registration_limit").(int)))
	}
	if len(tags) > 0 {
		activationInput.Tags = Tags(tags.IgnoreAWS())
	}

	// Retry to allow iam_role to be created and policy attachment to take place
	var resp *ssm.CreateActivationOutput
	err := retry.RetryContext(ctx, propagationTimeout, func() *retry.RetryError {
		var err error

		resp, err = conn.CreateActivationWithContext(ctx, activationInput)

		if tfawserr.ErrMessageContains(err, "ValidationException", "Not existing role") {
			return retry.RetryableError(err)
		}

		if err != nil {
			return retry.NonRetryableError(err)
		}

		return nil
	})

	if tfresource.TimedOut(err) {
		resp, err = conn.CreateActivationWithContext(ctx, activationInput)
	}

	if err != nil {
		return sdkdiag.AppendErrorf(diags, "creating SSM activation: %s", err)
	}

	if resp.ActivationId == nil {
		return sdkdiag.AppendErrorf(diags, "ActivationId was nil")
	}
	d.SetId(aws.StringValue(resp.ActivationId))
	d.Set("activation_code", resp.ActivationCode)

	return append(diags, resourceActivationRead(ctx, d, meta)...)
}

func resourceActivationRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	conn := meta.(*conns.AWSClient).SSMConn()
	defaultTagsConfig := meta.(*conns.AWSClient).DefaultTagsConfig
	ignoreTagsConfig := meta.(*conns.AWSClient).IgnoreTagsConfig

	log.Printf("[DEBUG] Reading SSM Activation: %s", d.Id())

	params := &ssm.DescribeActivationsInput{
		Filters: []*ssm.DescribeActivationsFilter{
			{
				FilterKey: aws.String("ActivationIds"),
				FilterValues: []*string{
					aws.String(d.Id()),
				},
			},
		},
		MaxResults: aws.Int64(1),
	}

	resp, err := conn.DescribeActivationsWithContext(ctx, params)

	if err != nil {
		return sdkdiag.AppendErrorf(diags, "reading SSM activation: %s", err)
	}
	if !d.IsNewResource() && (resp.ActivationList == nil || len(resp.ActivationList) == 0) {
		log.Printf("[WARN] SSM Activation (%s) not found, removing from state", d.Id())
		d.SetId("")
		return diags
	}

	activation := resp.ActivationList[0] // Only 1 result as MaxResults is 1 above
	d.Set("name", activation.DefaultInstanceName)
	d.Set("description", activation.Description)
	d.Set("expiration_date", aws.TimeValue(activation.ExpirationDate).Format(time.RFC3339))
	d.Set("expired", activation.Expired)
	d.Set("iam_role", activation.IamRole)
	d.Set("registration_limit", activation.RegistrationLimit)
	d.Set("registration_count", activation.RegistrationsCount)
	tags := KeyValueTags(ctx, activation.Tags).IgnoreAWS().IgnoreConfig(ignoreTagsConfig)

	//lintignore:AWSR002
	if err := d.Set("tags", tags.RemoveDefaultConfig(defaultTagsConfig).Map()); err != nil {
		return sdkdiag.AppendErrorf(diags, "setting tags: %s", err)
	}

	if err := d.Set("tags_all", tags.Map()); err != nil {
		return sdkdiag.AppendErrorf(diags, "setting tags_all: %s", err)
	}

	return diags
}

func resourceActivationDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	var diags diag.Diagnostics
	conn := meta.(*conns.AWSClient).SSMConn()

	log.Printf("[DEBUG] Deleting SSM Activation: %s", d.Id())

	params := &ssm.DeleteActivationInput{
		ActivationId: aws.String(d.Id()),
	}

	_, err := conn.DeleteActivationWithContext(ctx, params)

	if err != nil {
		return sdkdiag.AppendErrorf(diags, "deleting SSM activation: %s", err)
	}

	return diags
}
