// web/src/features/catalog/api/execute-command.ts
import {
  deleteCatalogItemsItemIdImage,
  patchCatalogCategoriesCategoryIdDetails,
  patchCatalogCategoriesCategoryIdName,
  patchCatalogItemsItemIdCategory,
  patchCatalogItemsItemIdDetails,
  patchCatalogItemsItemIdName,
  patchCatalogItemsItemIdPrice,
  patchCatalogModifierGroupsGroupIdName,
  patchCatalogModifierOptionsOptionIdName,
  patchCatalogModifierOptionsOptionIdPrice,
  patchCatalogSizesSizeIdName,
  patchCatalogSizesSizeIdPrice,
  postCatalogCategories,
  postCatalogCategoriesCategoryIdRetirement,
  postCatalogItems,
  postCatalogItemsItemIdRetirement,
  postCatalogItemsItemIdSizes,
  postCatalogModifierGroups,
  postCatalogModifierGroupsGroupIdOptions,
  postCatalogModifierGroupsGroupIdRetirement,
  postCatalogModifierOptionsOptionIdRetirement,
  postCatalogSizesSizeIdRetirement,
  putCatalogCategoriesCategoryIdModifierGroups,
  putCatalogItemsItemIdImage,
  putCatalogItemsItemIdModifierGroups,
  putCatalogModifierGroupsGroupIdAssignments,
  putCatalogModifierGroupsGroupIdSelectionRule,
} from "@/api/generated/endpoints/catalog/catalog";
import type { CatalogSetCategoryDetailsRequest, CatalogSetItemDetailsRequest } from "@/api/generated/models";
import { newRequestId } from "@/lib/command";
import { unwrap, unwrapNullable } from "@/lib/unwrap";
import type { Retirement } from "../lib/catalog-model";
import type { Executor } from "../lib/run-plan";

function retireBody(request_id: string, r: Retirement) {
  return { request_id, reason: r.reason, note: r.note.trim() };
}

/**
 * Maps one planned command to its endpoint. Each call is its own intent with a
 * fresh request_id; a retry re-plans against refreshed state rather than
 * replaying (ADR-062). Commands that return nothing useful use unwrapNullable,
 * so an empty `data` is not mistaken for a failure.
 */
export const executeCommand: Executor = async (cmd, pin) => {
  const request_id = newRequestId();
  const manager_pin = pin ?? undefined;
  switch (cmd.type) {
    case "item.create": {
      const item = unwrap(
        await postCatalogItems({
          request_id,
          manager_pin,
          category_id: cmd.categoryId,
          name: cmd.name,
          price_vnd: cmd.priceVnd,
          sizes: cmd.sizes?.map((s) => ({ name: s.name, price_vnd: s.priceVnd })),
        }),
      );
      return { created: "item", id: item.id ?? "" };
    }
    case "item.rename":
      unwrapNullable(await patchCatalogItemsItemIdName(cmd.itemId, { request_id, name: cmd.name }));
      return;
    case "item.move":
      unwrapNullable(await patchCatalogItemsItemIdCategory(cmd.itemId, { request_id, category_id: cmd.categoryId }));
      return;
    case "item.reprice":
      unwrapNullable(await patchCatalogItemsItemIdPrice(cmd.itemId, { request_id, manager_pin, price_vnd: cmd.priceVnd }));
      return;
    case "item.details": {
      // The server requires all three keys and reads null as "clear"; the
      // generated type has no null, hence the cast.
      const body = { request_id, code: cmd.code, badge: cmd.badge, description: cmd.description };
      unwrapNullable(await patchCatalogItemsItemIdDetails(cmd.itemId, body as unknown as CatalogSetItemDetailsRequest));
      return;
    }
    case "item.image.set":
      unwrapNullable(await putCatalogItemsItemIdImage(cmd.itemId, { request_id, file: cmd.blob }));
      return;
    case "item.image.clear":
      unwrapNullable(await deleteCatalogItemsItemIdImage(cmd.itemId, { request_id }));
      return;
    case "item.groups":
      unwrapNullable(
        await putCatalogItemsItemIdModifierGroups(cmd.itemId, {
          request_id,
          direct_group_ids: cmd.directGroupIds,
          excluded_group_ids: cmd.excludedGroupIds,
        }),
      );
      return;
    case "item.retire":
      unwrapNullable(await postCatalogItemsItemIdRetirement(cmd.itemId, retireBody(request_id, cmd.retirement)));
      return;
    case "size.rename":
      unwrapNullable(await patchCatalogSizesSizeIdName(cmd.sizeId, { request_id, name: cmd.name }));
      return;
    case "size.reprice":
      unwrapNullable(await patchCatalogSizesSizeIdPrice(cmd.sizeId, { request_id, manager_pin, price_vnd: cmd.priceVnd }));
      return;
    case "size.add":
      unwrapNullable(
        await postCatalogItemsItemIdSizes(cmd.itemId, { request_id, manager_pin, name: cmd.name, price_vnd: cmd.priceVnd }),
      );
      return;
    case "size.retire":
      unwrapNullable(await postCatalogSizesSizeIdRetirement(cmd.sizeId, retireBody(request_id, cmd.retirement)));
      return;
    case "category.create": {
      const category = unwrap(await postCatalogCategories({ request_id, name: cmd.name }));
      return { created: "category", id: category.id ?? "" };
    }
    case "category.rename":
      unwrapNullable(await patchCatalogCategoriesCategoryIdName(cmd.categoryId, { request_id, name: cmd.name }));
      return;
    case "category.details": {
      const body = { request_id, icon: cmd.icon, display_order: cmd.displayOrder };
      unwrapNullable(
        await patchCatalogCategoriesCategoryIdDetails(cmd.categoryId, body as unknown as CatalogSetCategoryDetailsRequest),
      );
      return;
    }
    case "category.groups":
      unwrapNullable(await putCatalogCategoriesCategoryIdModifierGroups(cmd.categoryId, { request_id, group_ids: cmd.groupIds }));
      return;
    case "category.retire":
      unwrapNullable(await postCatalogCategoriesCategoryIdRetirement(cmd.categoryId, retireBody(request_id, cmd.retirement)));
      return;
    case "group.create":
      unwrapNullable(
        await postCatalogModifierGroups({
          request_id,
          manager_pin,
          name: cmd.name,
          min_selections: cmd.min,
          max_selections: cmd.max,
          options: cmd.options.map((o) => ({ name: o.name, surcharge_vnd: o.surchargeVnd })),
          default_option_names: cmd.defaultOptionNames,
        }),
      );
      return;
    case "group.rename":
      unwrapNullable(await patchCatalogModifierGroupsGroupIdName(cmd.groupId, { request_id, name: cmd.name }));
      return;
    case "group.rule":
      unwrapNullable(
        await putCatalogModifierGroupsGroupIdSelectionRule(cmd.groupId, {
          request_id,
          min_selections: cmd.min,
          max_selections: cmd.max,
          default_option_ids: cmd.defaultOptionIds,
        }),
      );
      return;
    case "group.assignments":
      unwrapNullable(
        await putCatalogModifierGroupsGroupIdAssignments(cmd.groupId, {
          request_id,
          item_ids: cmd.itemIds,
          category_ids: cmd.categoryIds,
        }),
      );
      return;
    case "group.retire":
      unwrapNullable(await postCatalogModifierGroupsGroupIdRetirement(cmd.groupId, retireBody(request_id, cmd.retirement)));
      return;
    case "option.rename":
      unwrapNullable(await patchCatalogModifierOptionsOptionIdName(cmd.optionId, { request_id, name: cmd.name }));
      return;
    case "option.reprice":
      unwrapNullable(
        await patchCatalogModifierOptionsOptionIdPrice(cmd.optionId, { request_id, manager_pin, surcharge_vnd: cmd.surchargeVnd }),
      );
      return;
    case "option.add": {
      const option = unwrap(
        await postCatalogModifierGroupsGroupIdOptions(cmd.groupId, {
          request_id,
          manager_pin,
          name: cmd.name,
          surcharge_vnd: cmd.surchargeVnd,
        }),
      );
      return { created: "option", id: option.id ?? "", name: cmd.name };
    }
    case "option.retire":
      unwrapNullable(await postCatalogModifierOptionsOptionIdRetirement(cmd.optionId, retireBody(request_id, cmd.retirement)));
      return;
  }
};
