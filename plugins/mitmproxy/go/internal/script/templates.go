package script

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Template is a bundled or on-disk script template.
type Template struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source"`
}

var bundledTemplates = []Template{
	{
		ID:          "add-header",
		Name:        "Add request header",
		Description: "Sets X-Proxying-Script on the request",
		Source: `// onRequest
function onRequest(flow) {
  flow.request.headers["X-Proxying-Script"] = ["1"];
}
`,
	},
	{
		ID:          "strip-cookie",
		Name:        "Strip Cookie",
		Description: "Removes Cookie request header",
		Source: `// onRequest
function onRequest(flow) {
  delete flow.request.headers["Cookie"];
  delete flow.request.headers["cookie"];
}
`,
	},
	{
		ID:          "log-url",
		Name:        "Tag URL path",
		Description: "Appends path to a response header",
		Source: `// onResponse
function onResponse(flow) {
  if (flow.response && flow.request) {
    flow.response.headers["X-Captured-Path"] = [flow.request.url || ""];
  }
}
`,
	},

	// ---- QuantumultX-Rewrite 转换模板 ----
	// 来源: github.com/deezertidal/QuantumultX-Rewrite(277 个 rewrite conf 及其引用的
	// JS 脚本)。转换规则: $response.body -> flow.response.body, $done({body}) -> 直接
	// 赋值, conf 里的 ^url 匹配式 -> 模板开头的 URL 守卫(runner 对所有流执行, 不带上匹配)。
	// 每个模板都经过 node 功能验证(命中改写 + 未命中原样不动), 见 scripts/qx_convert
	// 说明与 AGENTS.md。原脚本版权归各作者, 仅供学习交流。
	{
		ID:          "qx-cubox",
		Name:        "Cubox 收藏阅读 — 解锁 VIP",
		Description: "QuantumultX-Rewrite 转换",
		Source: `// Cubox 收藏阅读 — 解锁 VIP
// 源: QuantumultX-Rewrite(github.com/deezertidal/QuantumultX-Rewrite) → rewrite/cubox.conf → ycdz/cubox.js (作者: 彭于晏)
// 匹配: ^https:\/\/cubox\.pro\/c\/api\/userInfo
function onResponse(flow) {
  if (flow.request.url.indexOf("/c/api/userInfo") === -1) return;
  var obj = JSON.parse(flow.response.body);
  obj.data.level = 1;
  obj.data.expireTime = "2099-09-12T23:50:23+08:00";
  obj.data.nickName = "Cubox VIP";
  obj.data.thirdNickName = "Cubox VIP";
  obj.data.isExpire = false;
  obj.data.active = true;
  obj.data.payTime = 1660006006;
  flow.response.body = JSON.stringify(obj);
}
`,
	},

	{
		ID:          "qx-calculator-hd",
		Name:        "计算器HD — 解锁会员",
		Description: "QuantumultX-Rewrite 转换",
		Source: `// 计算器HD — 解锁会员
// 源: QuantumultX-Rewrite(github.com/deezertidal/QuantumultX-Rewrite) → rewrite/calculator.conf → CalculatorHDProCrack.js (作者: Hausd0rff)
// 匹配: ^https?://www\.40sishi\.com/(list|currency|calculator)/user/profile$
function onResponse(flow) {
  if (!/www\.40sishi\.com\/(list|currency|calculator)\/user\/profile$/.test(flow.request.url)) return;
  var obj = JSON.parse(flow.response.body);
  obj.data.vipState = { state: 1, forever: true, startTime: 1569600000000, expireTime: 32495476149000 };
  flow.response.body = JSON.stringify(obj);
}
`,
	},

	{
		ID:          "qx-wallcraft",
		Name:        "WallCraft — 解锁专业版",
		Description: "QuantumultX-Rewrite 转换",
		Source: `// WallCraft — 解锁专业版
// 源: QuantumultX-Rewrite(github.com/deezertidal/QuantumultX-Rewrite) → rewrite/wallcraft.conf → WallCraftFProCrack.js (作者: yqc007)
// 匹配: ^https?://billing-ios\.wallpaperscraft\.com/verify_receipt/remove_ads$
function onResponse(flow) {
  if (!/billing-ios\.wallpaperscraft\.com\/verify_receipt\/remove_ads$/.test(flow.request.url)) return;
  var obj = JSON.parse(flow.response.body);
  obj.items.all_time = { type: "nonconsumable", is_active: true };
  flow.response.body = JSON.stringify(obj);
}
`,
	},

	{
		ID:          "qx-wps",
		Name:        "WPS Office — 解锁会员(稻壳/超级)",
		Description: "QuantumultX-Rewrite 转换",
		Source: `// WPS Office — 解锁会员(稻壳/超级)
// 源: QuantumultX-Rewrite(github.com/deezertidal/QuantumultX-Rewrite) → rewrite/WPS.conf → WPSOffice/Script/WPS.js (作者: R·E)
// 匹配: ^https?://.*?account\.wps\.(com|cn)(:\d+)?/api/users/\w+/overview$
function onResponse(flow) {
  if (!/account\.wps\.(com|cn)(:\d+)?\/api\/users\/\w+\/overview$/.test(flow.request.url)) return;
  var EXP = 1893456000; // 0x70db6800 = 2030-01-01, 原脚本写死的到期时间
  var obj = {
  "result": "ok",
  "privilege": [],
  "total_cost": 0,
  "level": 88,
  "wealth": 0,
  "exp": 0,
  "vip": {
    "has_ad": 0,
    "enabled": []
  },
  "total_buy": 0
};
  var privs = ["data_recover", "ocr", "pdf2doc", "pdf_merge", "pdf_sign", "pdf_split"];
  var tiers = [["超级会员", 40], ["WPS会员", 20], ["稻壳会员", 12]];
  obj.server_time = EXP;
  obj.userid = EXP;
  obj.vip.name = tiers[0][0];
  obj.vip.memberid = tiers[0][1];
  obj.vip.expire_time = EXP;
  for (var i = 0; i < privs.length; i++) obj.privilege[i] = { times: 0, expire_time: EXP, spid: privs[i] };
  for (var o = 0; o < tiers.length; o++) obj.vip.enabled[o] = { name: tiers[o][0], expire_time: EXP, memberid: tiers[o][1] };
  flow.response.body = JSON.stringify(obj);
}
`,
	},

	{
		ID:          "qx-audiomack",
		Name:        "AudioMack — 解锁永久订阅",
		Description: "QuantumultX-Rewrite 转换",
		Source: `// AudioMack — 解锁永久订阅
// 源: QuantumultX-Rewrite(github.com/deezertidal/QuantumultX-Rewrite) → rewrite/audiomack.conf → AudioMackFProCrack.js (作者: yqc007)
// 匹配: ^https?://api\.revenuecat\.com/v1/subscribers/(identify|\d+)$
function onResponse(flow) {
  if (!/api\.revenuecat\.com\/v1\/subscribers\/(identify|\d+)$/.test(flow.request.url)) return;
  flow.response.body = JSON.stringify({
  "request_date": "2021-01-24T09:52:51Z",
  "request_date_ms": 1611453171000,
  "subscriber": {
    "entitlements": {
      "Premium1": {
        "expires_date": "2999-09-28T09:00:00Z",
        "grace_period_expires_date": null,
        "product_identifier": "com.audiomack.premium.2018",
        "purchase_date": "2021-01-24T09:51:12Z"
      }
    },
    "first_seen": "2021-01-23T15:26:25Z",
    "last_seen": "2021-01-24T09:51:16Z",
    "management_url": "itms-apps://apps.apple.com/account/subscriptions",
    "non_subscriptions": {},
    "original_app_user_id": "$RCAnonymousID:98acaef952e6c42b364db358b3a48487",
    "original_application_version": "4396",
    "original_purchase_date": "2022-01-23T12:45:28Z",
    "other_purchases": {},
    "subscriptions": {
      "com.audiomack.premium.2018": {
        "billing_issues_detected_at": null,
        "expires_date": "2999-09-28T09:00:00Z",
        "grace_period_expires_date": null,
        "is_sandbox": false,
        "original_purchase_date": "2021-01-24T09:51:13Z",
        "ownership_type": "PURCHASED",
        "period_type": "premium",
        "purchase_date": "2021-01-24T09:51:12Z",
        "store": "app_store",
        "unsubscribe_detected_at": null
      }
    }
  }
});
}
`,
	},

	{
		ID:          "qx-bedtimefan",
		Name:        "Bedtime Fan — 解锁订阅",
		Description: "QuantumultX-Rewrite 转换",
		Source: `// Bedtime Fan — 解锁订阅
// 源: QuantumultX-Rewrite(github.com/deezertidal/QuantumultX-Rewrite) → rewrite/BedtimeFan.conf → bedtimeFan.js (作者: Marol62926)
// 匹配: https://api.revenuecat.com/v1/(receipts|subscribers)/*
function onResponse(flow) {
  if (!/api\.revenuecat\.com\/v1\/(receipts|subscribers)\//.test(flow.request.url)) return;
  var obj = JSON.parse(flow.response.body);
  obj.subscriber.entitlements = {
  "com.bedtimefan.entitlement.campfire": {
    "expires_date": "2029-05-08T15:24:07Z",
    "grace_period_expires_date": null,
    "product_identifier": "com.ellisapps.bedtimesleepfanlite.SubscriptionYearlyL19Y_F1W",
    "purchase_date": "2022-05-01T15:24:07Z"
  },
  "com.bedtimefan.entitlement.crickets": {
    "expires_date": "2029-05-08T15:24:07Z",
    "grace_period_expires_date": null,
    "product_identifier": "com.ellisapps.bedtimesleepfanlite.SubscriptionYearlyL19Y_F1W",
    "purchase_date": "2022-05-01T15:24:07Z"
  },
  "com.bedtimefan.entitlement.premium": {
    "expires_date": "2029-05-08T15:24:07Z",
    "grace_period_expires_date": null,
    "product_identifier": "com.ellisapps.bedtimesleepfanlite.SubscriptionYearlyL19Y_F1W",
    "purchase_date": "2022-05-01T15:24:07Z"
  },
  "com.bedtimefan.entitlement.rain": {
    "expires_date": "2029-05-08T15:24:07Z",
    "grace_period_expires_date": null,
    "product_identifier": "com.ellisapps.bedtimesleepfanlite.SubscriptionYearlyL19Y_F1W",
    "purchase_date": "2022-05-01T15:24:07Z"
  },
  "com.bedtimefan.entitlement.removeAds": {
    "expires_date": "2029-05-08T15:24:07Z",
    "grace_period_expires_date": null,
    "product_identifier": "com.ellisapps.bedtimesleepfanlite.SubscriptionYearlyL19Y_F1W",
    "purchase_date": "2022-05-01T15:24:07Z"
  }
};
  flow.response.body = JSON.stringify(obj);
}
`,
	},

	{
		ID:          "qx-emby",
		Name:        "Emby Premiere — 设备验证解锁",
		Description: "QuantumultX-Rewrite 转换",
		Source: `// Emby Premiere — 设备验证解锁
// 源: QuantumultX-Rewrite(github.com/deezertidal/QuantumultX-Rewrite) → rewrite/Emby.conf → Emby.js (作者: Tartarus2014)
// 匹配: ^https?://mb3admin\.com/admin/service/registration/validateDevice
function onResponse(flow) {
  if (flow.request.url.indexOf("mb3admin.com/admin/service/registration/validateDevice") === -1) return;
  if (flow.response.status === 200) return; // 已通过的不动
  flow.response.status = 200;
  flow.response.headers["Content-Type"] = "application/json";
  flow.response.body = JSON.stringify({
    cacheExpirationDays: 999,
    resultCode: "GOOD",
    message: "Device Valid"
  });
}
`,
	},

	{
		ID:          "qx-dayone",
		Name:        "Day One — 解锁 Premium",
		Description: "QuantumultX-Rewrite 转换",
		Source: `// Day One — 解锁 Premium
// 源: QuantumultX-Rewrite(github.com/deezertidal/QuantumultX-Rewrite) → rewrite/dayone.conf → ycdz/dayone.js (作者: 89996462)
// 匹配: https://dayone.app/api/v2/users/(receipt|account-status)
function onResponse(flow) {
  if (!/dayone\.app\/api\/v2\/users\/(receipt|account-status)/.test(flow.request.url)) return;
  var url = flow.request.url;
  var obj;
  if (url.indexOf("/account-status") !== -1) {
    obj = {
  "expirationDate": 1866888955000,
  "startDate": 1582805832000,
  "subscriptionPlan": "com.bloombuilt.dayoneios.subscription.premium.yearly_discounted_trial",
  "lastRenewalDate": 1582805831000,
  "subscriptionName": "premium",
  "bundleReason": "purchase",
  "cancellationDate": 0
};
  }
  if (url.indexOf("/receipt") !== -1) {
    obj = {
  "bundle": {
    "bundleName": "premium",
    "features": [
      {
        "name": "imagesPerEntry",
        "limit": 30,
        "canUpgrade": false
      },
      {
        "name": "journalLimit",
        "limit": 100,
        "canUpgrade": false
      },
      {
        "name": "audioPerEntry",
        "limit": 10,
        "canUpgrade": false
      },
      {
        "name": "sync",
        "limit": null,
        "canUpgrade": false
      },
      {
        "name": "backup",
        "limit": null,
        "canUpgrade": false
      },
      {
        "name": "printingDiscount",
        "limit": null,
        "canUpgrade": false
      },
      {
        "name": "prioritySupport",
        "limit": null,
        "canUpgrade": false
      },
      {
        "name": "drawingsPerEntry",
        "limit": 30,
        "canUpgrade": false
      },
      {
        "name": "scanToPDF",
        "limit": 30,
        "canUpgrade": false
      },
      {
        "name": "videosPerEntry",
        "limit": 30,
        "canUpgrade": false
      },
      {
        "name": "journalViaSMS",
        "limit": null,
        "canUpgrade": false
      }
    ],
    "featuresFull": [
      {
        "name": "journalLimit",
        "limit": 100,
        "canUpgrade": false
      },
      {
        "name": "backup",
        "enabled": true,
        "canUpgrade": false
      },
      {
        "name": "sync",
        "enabled": true,
        "canUpgrade": false
      },
      {
        "name": "discountedSubscription",
        "enabled": false,
        "canUpgrade": false
      },
      {
        "name": "printingDiscount",
        "enabled": true,
        "canUpgrade": false
      },
      {
        "name": "prioritySupport",
        "enabled": true,
        "canUpgrade": false
      },
      {
        "name": "scanToPDF",
        "enabled": true,
        "canUpgrade": false
      },
      {
        "name": "attachmentsPerEntry",
        "limit": 30,
        "canUpgrade": false
      },
      {
        "name": "journalViaSMS",
        "enabled": true,
        "canUpgrade": false
      },
      {
        "name": "canAttachPhoto",
        "enabled": true,
        "canUpgrade": false
      },
      {
        "name": "canAttachVideo",
        "enabled": true,
        "canUpgrade": false
      },
      {
        "name": "canAttachAudio",
        "enabled": true,
        "canUpgrade": false
      },
      {
        "name": "canAttachDrawing",
        "enabled": true,
        "canUpgrade": false
      },
      {
        "name": "canAttachPDF",
        "enabled": true,
        "canUpgrade": false
      },
      {
        "name": "instagram",
        "enabled": true,
        "canUpgrade": false
      },
      {
        "name": "ifttt",
        "enabled": true,
        "canUpgrade": false
      },
      {
        "name": "colors",
        "enabled": true,
        "canUpgrade": false
      },
      {
        "name": "emailToJournal",
        "enabled": true,
        "canUpgrade": false
      },
      {
        "name": "appIcons",
        "enabled": true,
        "canUpgrade": false
      },
      {
        "name": "moonPhase",
        "enabled": true,
        "canUpgrade": false
      },
      {
        "name": "foursquareNearbyVenues",
        "enabled": true,
        "canUpgrade": false
      }
    ]
  },
  "reason": "purchase"
};
  }
  flow.response.body = JSON.stringify(obj);
}
`,
	},

	{
		ID:          "qx-grammarly",
		Name:        "Grammarly — 解锁 Premium 订阅",
		Description: "QuantumultX-Rewrite 转换",
		Source: `// Grammarly — 解锁 Premium 订阅
// 源: QuantumultX-Rewrite(github.com/deezertidal/QuantumultX-Rewrite) → rewrite/grammarly.conf → GrammarlyPremiumCrack.js (作者: yqc007)
// 匹配: ^https?://subscription\.grammarly\.com/api/v1/subscription$
function onResponse(flow) {
  if (!/subscription\.grammarly\.com\/api\/v1\/subscription$/.test(flow.request.url)) return;
  flow.response.body = JSON.stringify({
  "isPremium": true,
  "nextPayDate": "Oct 28, 2999 4:56:52 AM",
  "isCancelled": false,
  "isDunning": false,
  "isPaymentMethodFailed": false,
  "isFree": false,
  "canCancelPlanSwitch": false,
  "isFreebie": false,
  "isPremiumConversion": true,
  "isLegacyPayPal": false,
  "isOnTrial": false,
  "currentPlan": {
    "id": 1005,
    "regularPlanId": 1005,
    "title": "Annual",
    "description": "1-year Grammarly Subscription",
    "regularPrice": 144.0,
    "regularPriceMoney": {
      "currency": "USD",
      "value": 144.0
    },
    "price": 144.0,
    "priceMoney": {
      "currency": "USD",
      "value": 144.0
    },
    "periodMonths": 12,
    "hasTrial": false,
    "trialDays": 0,
    "firstThreeMonthsPromo": 0,
    "baseInstitutionCampaign": false
  },
  "plans": [
    {
      "id": 1001,
      "regularPlanId": 1001,
      "title": "Monthly",
      "description": "1-month Grammarly Subscription",
      "regularPrice": 30.0,
      "regularPriceMoney": {
        "currency": "USD",
        "value": 30.0
      },
      "price": 30.0,
      "priceMoney": {
        "currency": "USD",
        "value": 30.0
      },
      "periodMonths": 1,
      "hasTrial": false,
      "trialDays": 0,
      "firstThreeMonthsPromo": 0,
      "baseInstitutionCampaign": false
    },
    {
      "id": 1003,
      "regularPlanId": 1003,
      "title": "Quarterly",
      "description": "3-month Grammarly Subscription",
      "regularPrice": 60.0,
      "regularPriceMoney": {
        "currency": "USD",
        "value": 60.0
      },
      "price": 60.0,
      "priceMoney": {
        "currency": "USD",
        "value": 60.0
      },
      "periodMonths": 3,
      "hasTrial": false,
      "trialDays": 0,
      "firstThreeMonthsPromo": 0,
      "baseInstitutionCampaign": false
    },
    {
      "id": 1005,
      "regularPlanId": 1005,
      "title": "Annual",
      "description": "1-year Grammarly Subscription",
      "regularPrice": 144.0,
      "regularPriceMoney": {
        "currency": "USD",
        "value": 144.0
      },
      "price": 144.0,
      "priceMoney": {
        "currency": "USD",
        "value": 144.0
      },
      "periodMonths": 12,
      "hasTrial": false,
      "trialDays": 0,
      "firstThreeMonthsPromo": 0,
      "baseInstitutionCampaign": false
    }
  ],
  "institutionPlans": [
    {
      "id": 10200462,
      "regularPlanId": 10200462,
      "title": "Monthly",
      "description": "1-month Grammarly Subscription",
      "regularPrice": 25.0,
      "regularPriceMoney": {
        "currency": "USD",
        "value": 25.0
      },
      "price": 25.0,
      "priceMoney": {
        "currency": "USD",
        "value": 25.0
      },
      "periodMonths": 1,
      "hasTrial": false,
      "trialDays": 0,
      "firstThreeMonthsPromo": 0,
      "baseInstitutionCampaign": true
    },
    {
      "id": 10200487,
      "regularPlanId": 10200487,
      "title": "Monthly",
      "description": "1-month Grammarly Subscription",
      "regularPrice": 25.0,
      "regularPriceMoney": {
        "currency": "USD",
        "value": 25.0
      },
      "price": 25.0,
      "priceMoney": {
        "currency": "USD",
        "value": 25.0
      },
      "periodMonths": 1,
      "hasTrial": true,
      "trialDays": 7,
      "firstThreeMonthsPromo": 0,
      "baseInstitutionCampaign": true
    },
    {
      "id": 10200461,
      "regularPlanId": 10200461,
      "title": "Annual",
      "description": "1-year Grammarly Subscription",
      "regularPrice": 180.0,
      "regularPriceMoney": {
        "currency": "USD",
        "value": 180.0
      },
      "price": 180.0,
      "priceMoney": {
        "currency": "USD",
        "value": 180.0
      },
      "periodMonths": 12,
      "hasTrial": false,
      "trialDays": 0,
      "firstThreeMonthsPromo": 0,
      "baseInstitutionCampaign": true
    },
    {
      "id": 10200486,
      "regularPlanId": 10200486,
      "title": "Annual",
      "description": "1-year Grammarly Subscription",
      "regularPrice": 180.0,
      "regularPriceMoney": {
        "currency": "USD",
        "value": 180.0
      },
      "price": 180.0,
      "priceMoney": {
        "currency": "USD",
        "value": 180.0
      },
      "periodMonths": 12,
      "hasTrial": true,
      "trialDays": 7,
      "firstThreeMonthsPromo": 0,
      "baseInstitutionCampaign": true
    }
  ],
  "institutionEduPlans": [
    {
      "id": 10201059,
      "regularPlanId": 10201059,
      "title": "flat_edu_plan",
      "description": "A plan for EDU with flat pricing per seat.",
      "regularPrice": 50.0,
      "regularPriceMoney": {
        "currency": "USD",
        "value": 50.0
      },
      "price": 50.0,
      "priceMoney": {
        "currency": "USD",
        "value": 50.0
      },
      "periodMonths": 12,
      "hasTrial": false,
      "trialDays": 0,
      "firstThreeMonthsPromo": 0,
      "baseInstitutionCampaign": false
    }
  ],
  "institutionDynamicPlans": [
    {
      "id": 10200578,
      "regularPlanId": 10200578,
      "title": "Annual",
      "description": "Annual tiered plan",
      "regularPrice": 150.0,
      "regularPriceMoney": {
        "currency": "USD",
        "value": 150.0
      },
      "price": 150.0,
      "priceMoney": {
        "currency": "USD",
        "value": 150.0
      },
      "periodMonths": 12,
      "hasTrial": false,
      "trialDays": 0,
      "firstThreeMonthsPromo": 0,
      "baseInstitutionCampaign": true,
      "priceTiers": [
        {
          "price": 150.0,
          "priceMoney": {
            "currency": "USD",
            "value": 150.0
          },
          "fromSeats": 0,
          "toSeats": 9
        },
        {
          "price": 145.0,
          "priceMoney": {
            "currency": "USD",
            "value": 145.0
          },
          "fromSeats": 10,
          "toSeats": 49
        },
        {
          "price": 140.0,
          "priceMoney": {
            "currency": "USD",
            "value": 140.0
          },
          "fromSeats": 50,
          "toSeats": 149
        }
      ]
    },
    {
      "id": 10200582,
      "regularPlanId": 10200582,
      "title": "Annual",
      "description": "Annual tiered plan",
      "regularPrice": 150.0,
      "regularPriceMoney": {
        "currency": "USD",
        "value": 150.0
      },
      "price": 150.0,
      "priceMoney": {
        "currency": "USD",
        "value": 150.0
      },
      "periodMonths": 12,
      "hasTrial": true,
      "trialDays": 7,
      "firstThreeMonthsPromo": 0,
      "baseInstitutionCampaign": true,
      "priceTiers": [
        {
          "price": 150.0,
          "priceMoney": {
            "currency": "USD",
            "value": 150.0
          },
          "fromSeats": 0,
          "toSeats": 9
        },
        {
          "price": 145.0,
          "priceMoney": {
            "currency": "USD",
            "value": 145.0
          },
          "fromSeats": 10,
          "toSeats": 49
        },
        {
          "price": 140.0,
          "priceMoney": {
            "currency": "USD",
            "value": 140.0
          },
          "fromSeats": 50,
          "toSeats": 149
        }
      ]
    }
  ],
  "pricingOptions": {
    "discountSuppressed": false,
    "taxDisplayType": "none",
    "individualSubscriptionExclusivePricingEnabled": false,
    "renewalPriceType": "INCLUSIVE"
  },
  "paymentMethodOptions": {
    "payPalDisabled": false,
    "americanExpressDisabled": false,
    "disabledCardTypes": []
  },
  "paymentMethod": "creditCard",
  "subscriptionPaymentMethods": [
    "creditCard"
  ],
  "creditCard": {
    "cardType": "Visa"
  },
  "isAppleSubscription": false,
  "isGooglePlaySubscription": false,
  "isSamsungPlan": false,
  "customerSince": "Aug 27, 2020 8:11:05 PM",
  "lastSubscribedDate": "Oct 20, 2021 4:56:55 AM",
  "state": "DEFAULT"
});
}
`,
	},

	{
		ID:          "qx-tokcam",
		Name:        "TOK CAM — 解锁永久订阅",
		Description: "QuantumultX-Rewrite 转换",
		Source: `// TOK CAM — 解锁永久订阅
// 源: QuantumultX-Rewrite(github.com/deezertidal/QuantumultX-Rewrite) → rewrite/tokcam.conf → TokCamFProCrack.js (作者: yqc007)
// 匹配: ^https?://buy\.itunes\.apple\.com/verifyReceipt$
function onResponse(flow) {
  if (!/buy\.itunes\.apple\.com\/verifyReceipt$/.test(flow.request.url)) return;
  flow.response.body = JSON.stringify({
  "receipt": {
    "receipt_type": "Production",
    "app_item_id": 1522192046,
    "receipt_creation_date": "2022-02-11 02:26:22 Etc/GMT",
    "bundle_id": "com.muyin.camera",
    "original_purchase_date": "2020-09-09 01:01:59 Etc/GMT",
    "in_app": [
      {
        "quantity": "1",
        "purchase_date_ms": "1642850859000",
        "transaction_id": "350001102215973",
        "is_trial_period": "false",
        "original_transaction_id": "350001102215973",
        "purchase_date": "2022-01-22 11:27:39 Etc/GMT",
        "product_id": "incentive01",
        "original_purchase_date_pst": "2022-01-22 03:27:39 America/Los_Angeles",
        "in_app_ownership_type": "PURCHASED",
        "original_purchase_date_ms": "1642850859000",
        "purchase_date_pst": "2022-01-22 03:27:39 America/Los_Angeles",
        "original_purchase_date": "2022-01-22 11:27:39 Etc/GMT"
      }
    ],
    "adam_id": 1522192046,
    "receipt_creation_date_pst": "2022-02-10 18:26:22 America/Los_Angeles",
    "request_date": "2022-02-11 02:28:49 Etc/GMT",
    "request_date_pst": "2022-02-10 18:28:49 America/Los_Angeles",
    "version_external_identifier": 846803077,
    "request_date_ms": "1644546529150",
    "original_purchase_date_pst": "2020-09-08 18:01:59 America/Los_Angeles",
    "application_version": "202201230001",
    "original_purchase_date_ms": "1599613319000",
    "receipt_creation_date_ms": "1644546382000",
    "original_application_version": "2020090601",
    "download_id": 75077048258874
  },
  "status": 0,
  "environment": "Production"
});
}
`,
	},
}

// Gallery lists bundled templates and can apply them into the scripts dir.
type Gallery struct {
	mu  sync.RWMutex
	dir string // dataDir/scripts
}

// NewGallery uses the runner scripts directory.
func NewGallery(scriptsDir string) *Gallery {
	return &Gallery{dir: scriptsDir}
}

// List returns bundled templates (id/name/description; source included).
func (g *Gallery) List() []Template {
	out := make([]Template, len(bundledTemplates))
	copy(out, bundledTemplates)
	return out
}

// Get returns a template by id.
func (g *Gallery) Get(id string) (Template, bool) {
	id = strings.TrimSpace(id)
	for _, t := range bundledTemplates {
		if t.ID == id {
			return t, true
		}
	}
	return Template{}, false
}

// Apply writes template source as name.js under scripts dir. name defaults to id.js.
func (g *Gallery) Apply(id, fileName string) (string, error) {
	t, ok := g.Get(id)
	if !ok {
		return "", fmt.Errorf("template %q not found", id)
	}
	if fileName == "" {
		fileName = t.ID + ".js"
	}
	if !strings.HasSuffix(strings.ToLower(fileName), ".js") {
		fileName += ".js"
	}
	fileName = filepath.Base(fileName)
	if fileName == "." || fileName == ".." || strings.Contains(fileName, string(filepath.Separator)) {
		return "", fmt.Errorf("invalid file name")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := os.MkdirAll(g.dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(g.dir, fileName)
	if err := os.WriteFile(path, []byte(t.Source), 0o600); err != nil {
		return "", err
	}
	return fileName, nil
}

// ScriptsDir returns the scripts directory.
func (g *Gallery) ScriptsDir() string { return g.dir }

// ListInstalled returns *.js basenames in the scripts dir.
func (r *Runner) ListInstalled() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.scripts))
	for _, s := range r.scripts {
		out = append(out, s.name)
	}
	return out
}

// Dir returns the scripts directory path.
func (r *Runner) Dir() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.dir
}
