import ast
import asyncio
import contextlib
import io
from pathlib import Path
import unittest


SCRIPT = Path(__file__).with_name("vpc_subnets.py")


class APIError(Exception):
    code = "InvalidSubnet.NotFound"
    status = "failed"
    request_id = "test-request"


async def execute(call_api, args=None):
    namespace = {"call_api": call_api, "APIError": APIError, "args": args}
    code = compile(
        SCRIPT.read_text(), str(SCRIPT), "exec", flags=ast.PyCF_ALLOW_TOP_LEVEL_AWAIT
    )
    with contextlib.redirect_stdout(io.StringIO()):
        await eval(code, namespace)
    return namespace["result"]


class VpcSubnetsTests(unittest.IsolatedAsyncioTestCase):
    async def test_pagination_selection_and_parallel_details(self):
        calls, entered = [], set()
        ready = asyncio.Event()

        async def call_api(service, action, parameters):
            self.assertEqual(service, "vpc")
            calls.append((action, parameters.copy()))
            if action == "DescribeVpcs":
                if "NextToken" not in parameters:
                    return {"Result": {"Vpcs": [{"VpcId": "empty", "SubnetIds": []}], "NextToken": "vpc-page-2"}}
                self.assertEqual(parameters["NextToken"], "vpc-page-2")
                return {"Result": {"Vpcs": [{"VpcId": "chosen", "SubnetIds": ["a", "b"]}, {"VpcId": "later", "SubnetIds": ["c"]}]}}
            if action == "DescribeSubnets":
                self.assertEqual(parameters["VpcId"], "chosen")
                if "NextToken" not in parameters:
                    return {"Result": {"Subnets": [{"SubnetId": "a"}], "NextToken": "subnet-page-2"}}
                self.assertEqual(parameters["NextToken"], "subnet-page-2")
                return {"Result": {"Subnets": [{"SubnetId": "b"}]}}
            self.assertEqual(action, "DescribeSubnetAttributes")
            entered.add(parameters["SubnetId"])
            if len(entered) == 2:
                ready.set()
            await asyncio.wait_for(ready.wait(), timeout=2)
            return {"Result": {"SubnetId": parameters["SubnetId"], "CidrBlock": "10.0.0.0/24"}}

        result = await execute(call_api)
        self.assertEqual(result["selected_vpc"]["VpcId"], "chosen")
        self.assertEqual(len(result["vpcs"]), 3)
        self.assertEqual([detail["SubnetId"] for detail in result["subnet_details"]], ["a", "b"])
        self.assertEqual(result["subnet_detail_errors"], 0)
        self.assertEqual(len(calls), 6)
        for action, parameters in calls:
            if action != "DescribeSubnetAttributes":
                self.assertEqual(parameters["MaxResults"], 100)

    async def test_no_vpc_with_subnets(self):
        async def call_api(service, action, parameters):
            self.assertEqual(action, "DescribeVpcs")
            return {"Result": {"Vpcs": [{"VpcId": "empty", "SubnetIds": None}]}}

        result = await execute(call_api)
        self.assertIsNone(result["selected_vpc"])
        self.assertEqual(result["subnet_details"], [])
        self.assertIn("No VPC", result["message"])

    async def test_one_failed_detail_preserves_other_results(self):
        async def call_api(service, action, parameters):
            if action == "DescribeVpcs":
                return {"Result": {"Vpcs": [{"VpcId": "chosen", "SubnetIds": ["a", "b"]}]}}
            if action == "DescribeSubnets":
                return {"Result": {"Subnets": [{"SubnetId": "a"}, {"SubnetId": "b"}]}}
            if parameters["SubnetId"] == "b":
                raise APIError("subnet was deleted")
            return {"Result": {"SubnetId": "a"}}

        result = await execute(call_api)
        self.assertEqual(result["subnet_detail_errors"], 1)
        self.assertEqual(result["subnet_details"][0]["SubnetId"], "a")
        self.assertEqual(result["subnet_details"][1]["Error"]["Code"], "InvalidSubnet.NotFound")

    async def test_repeated_pagination_token_fails_instead_of_looping(self):
        async def call_api(service, action, parameters):
            return {"Result": {"Vpcs": [], "NextToken": "repeated"}}

        with self.assertRaisesRegex(RuntimeError, "repeated pagination token"):
            await execute(call_api)

    async def test_detail_budget_reports_skipped_subnets(self):
        detail_calls = []

        async def call_api(service, action, parameters):
            if action == "DescribeVpcs":
                return {"Result": {"Vpcs": [{"VpcId": "chosen", "SubnetIds": ["a", "b", "c"]}]}}
            if action == "DescribeSubnets":
                return {"Result": {"Subnets": [{"SubnetId": value} for value in "abc"]}}
            detail_calls.append(parameters["SubnetId"])
            return {"Result": {"SubnetId": parameters["SubnetId"]}}

        result = await execute(call_api, {"api_budget": 3, "max_details": 50})
        self.assertEqual(len(result["subnets"]), 3)
        self.assertEqual(detail_calls, ["a"])
        self.assertTrue(result["subnet_details_truncated"])
        self.assertEqual(result["subnet_details_skipped"], 2)

    async def test_subnets_deleted_after_vpc_listing_are_explained(self):
        async def call_api(service, action, parameters):
            if action == "DescribeVpcs":
                return {"Result": {"Vpcs": [{"VpcId": "chosen", "SubnetIds": ["a"]}]}}
            self.assertEqual(action, "DescribeSubnets")
            return {"Result": {"Subnets": []}}

        result = await execute(call_api)
        self.assertEqual(result["selected_vpc"]["VpcId"], "chosen")
        self.assertEqual(result["subnet_details"], [])
        self.assertIn("no subnets", result["message"])


if __name__ == "__main__":
    unittest.main()
