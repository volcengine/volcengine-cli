"""List VPCs and inspect the first VPC with subnets using ve script run."""

import asyncio


options = args or {}
if not isinstance(options, dict):
    raise TypeError("args must be an object")
# Keep api_budget aligned with ve script run --max-calls (default: 100).
api_budget = options.get("api_budget", 100)
max_details = options.get("max_details", 50)
if isinstance(api_budget, bool) or not isinstance(api_budget, int) or not 1 <= api_budget <= 1000:
    raise ValueError("api_budget must be an integer between 1 and 1000")
if isinstance(max_details, bool) or not isinstance(max_details, int) or not 0 <= max_details <= 1000:
    raise ValueError("max_details must be an integer between 0 and 1000")
list_calls = 0


async def list_all(action, collection, filters=None):
    global list_calls
    items, token, seen_tokens = [], "", set()
    while True:
        if list_calls >= api_budget:
            raise RuntimeError("Pagination exhausted api_budget; increase args.api_budget and --max-calls together")
        parameters = dict(filters or {})
        parameters["MaxResults"] = 100
        if token:
            parameters["NextToken"] = token
        list_calls += 1
        response = await call_api("vpc", action, parameters)
        page = response["Result"]
        items.extend(page[collection] or [])
        token = page.get("NextToken") or ""
        if not token:
            return items
        if token in seen_tokens:
            raise RuntimeError(f"{action} returned a repeated pagination token")
        seen_tokens.add(token)


def vpc_summary(vpc):
    return {
        "VpcId": vpc["VpcId"],
        "VpcName": vpc.get("VpcName", ""),
        "CidrBlock": vpc.get("CidrBlock", ""),
        "SubnetCount": len(vpc.get("SubnetIds") or []),
    }


async def describe_subnet(subnet):
    subnet_id = subnet["SubnetId"]
    try:
        response = await call_api(
            "vpc", "DescribeSubnetAttributes", {"SubnetId": subnet_id}
        )
        return response["Result"]
    except APIError as error:
        return {
            "SubnetId": subnet_id,
            "Error": {
                "Code": error.code,
                "Message": str(error),
                "Status": error.status,
                "RequestId": error.request_id,
            },
        }


vpcs = await list_all("DescribeVpcs", "Vpcs")
selected = None
for vpc in vpcs:
    summary = vpc_summary(vpc)
    print(f"VPC {summary['VpcName']} ({summary['VpcId']}): {summary['SubnetCount']} subnet(s)")
    if selected is None and vpc.get("SubnetIds"):
        selected = vpc

result = {
    "vpcs": [vpc_summary(vpc) for vpc in vpcs],
    "selected_vpc": None,
    "subnets": [],
    "subnet_details": [],
    "subnet_detail_errors": 0,
    "subnet_details_truncated": False,
    "subnet_details_skipped": 0,
}
if selected is None:
    result["message"] = "No VPC with subnets was found in the current region."
    print(result["message"])
else:
    print(f"Selected VPC: {selected.get('VpcName', '')} ({selected['VpcId']})")
    subnets = await list_all("DescribeSubnets", "Subnets", {"VpcId": selected["VpcId"]})
    detail_count = min(len(subnets), max_details, api_budget - list_calls)
    details = await asyncio.gather(*(describe_subnet(subnet) for subnet in subnets[:detail_count]))
    result.update(
        selected_vpc=vpc_summary(selected),
        subnets=subnets,
        subnet_details=details,
        subnet_detail_errors=sum("Error" in detail for detail in details),
        subnet_details_truncated=detail_count < len(subnets),
        subnet_details_skipped=len(subnets) - detail_count,
    )
    if not subnets:
        result["message"] = "The selected VPC currently has no subnets; its SubnetIds may have changed after listing."
    elif result["subnet_details_truncated"]:
        result["message"] = "All subnets are listed; extra detail queries were capped by max_details or the remaining API budget."
    print(f"Queried {len(details)} subnet(s); {result['subnet_detail_errors']} detail error(s).")
