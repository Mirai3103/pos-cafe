/**
 * Seed data for the development database: demo staff, sample tables, the
 * full demo menu (categories, modifier groups, 76 items), and the image
 * file names under resources/seeds/ ({slug}.jpg).
 *
 * Codes are unique among active items (^[a-z0-9]{1,12}$ after lowercasing).
 * Sized items are fixed at creation (ADR-060) — at least one sized item must
 * exist so UAT can exercise POST /items/{id}/sizes.
 */

export interface SeedSize {
  name: string;
  priceVnd: number;
}

export interface SeedItem {
  /** Image file name base under resources/seeds/ (slugify of the name). */
  slug: string;
  name: string;
  category: string;
  /** Short display code shown on availability cards. */
  code: string;
  /** Single-price items. Mutually exclusive with sizes (ADR-060). */
  priceVnd?: number;
  /** Sized items. Mutually exclusive with priceVnd (ADR-060). */
  sizes?: SeedSize[];
  badge?: "BEST_SELLER" | "HOT" | "NEW" | "SIGNATURE" | "CHEF_PICK";
  description: string;
}

export interface SeedCategory {
  name: string;
  /** Lucide icon name (^[a-z0-9-]{1,40}$). */
  icon: string;
}

export interface SeedModifierGroup {
  name: string;
  minSelections: number;
  maxSelections: number;
  options: Array<{ name: string; surchargeVnd: number }>;
  defaultOptionNames?: string[];
  /** Category names the group is attached to. */
  categories: string[];
}

export const PIN = "1234";

export const seedStaff: Array<{
  displayName: string;
  loginCode: string;
  roles: string[];
  pin: string;
  /** The first Manager is created through /auth/bootstrap. */
  bootstrap?: boolean;
}> = [
  { displayName: "Quản lý Demo", loginCode: "QL01", roles: ["MANAGER"], pin: PIN, bootstrap: true },
  { displayName: "Thu ngân 01", loginCode: "TH01", roles: ["CASHIER"], pin: PIN },
  { displayName: "Pha chế 01", loginCode: "PB01", roles: ["BARISTA"], pin: PIN },
];

export const seedTables: string[] = Array.from({ length: 8 }, (_, i) => `Bàn ${i + 1}`);

export const seedCategories: SeedCategory[] = [
  { name: "Cà phê Việt", icon: "coffee" },
  { name: "Cà phê Âu", icon: "bean" },
  { name: "Matcha & Chocolate", icon: "leaf" },
  { name: "Trà & Trà trái cây", icon: "citrus" },
  { name: "Trà sữa", icon: "milk" },
  { name: "Đá xay", icon: "glass-water" },
  { name: "Sinh tố", icon: "cherry" },
  { name: "Nước ép", icon: "apple" },
  { name: "Soda & Mojito", icon: "cup-soda" },
  { name: "Sữa chua & Kem", icon: "ice-cream-bowl" },
  { name: "Đồ ăn nhẹ", icon: "croissant" },
];

const DRINK_CATEGORIES = seedCategories.filter((c) => c.name !== "Đồ ăn nhẹ").map((c) => c.name);

export const seedModifierGroups: SeedModifierGroup[] = [
  {
    name: "Mức đường",
    minSelections: 1,
    maxSelections: 1,
    options: [
      { name: "100% đường", surchargeVnd: 0 },
      { name: "70% đường", surchargeVnd: 0 },
      { name: "50% đường", surchargeVnd: 0 },
      { name: "Không đường", surchargeVnd: 0 },
    ],
    defaultOptionNames: ["100% đường"],
    categories: DRINK_CATEGORIES,
  },
  {
    name: "Topping thêm",
    minSelections: 0,
    maxSelections: 3,
    options: [
      { name: "Trân châu trắng", surchargeVnd: 5000 },
      { name: "Thạch nha đam", surchargeVnd: 5000 },
      { name: "Kem phô mai", surchargeVnd: 10000 },
    ],
    categories: DRINK_CATEGORIES,
  },
];

export const seedItems: SeedItem[] = [
  // === Cà phê Việt ===
  { slug: "ca-phe-den-da", name: "Cà phê đen đá", category: "Cà phê Việt", code: "CFDD", sizes: [{ name: "Size S", priceVnd: 25000 }, { name: "Size M", priceVnd: 29000 }, { name: "Size L", priceVnd: 35000 }], description: "Cà phê phin truyền thống đậm đà, thưởng lạnh cùng đá." },
  { slug: "ca-phe-den-nong", name: "Cà phê đen nóng", category: "Cà phê Việt", code: "CFDN", priceVnd: 25000, description: "Cà phê phin nóng nguyên bản, đậm vị Việt." },
  { slug: "ca-phe-sua-da", name: "Cà phê sữa đá", category: "Cà phê Việt", code: "CFSD", badge: "BEST_SELLER", sizes: [{ name: "Size S", priceVnd: 29000 }, { name: "Size M", priceVnd: 35000 }, { name: "Size L", priceVnd: 42000 }], description: "Cà phê sữa đặc hòa quyện, món quốc dân của mọi quán." },
  { slug: "ca-phe-sua-nong", name: "Cà phê sữa nóng", category: "Cà phê Việt", code: "CFSN", priceVnd: 29000, description: "Cà phê sữa đặc nóng, ngọt béo ấm áp." },
  { slug: "bac-xiu", name: "Bạc xỉu", category: "Cà phê Việt", code: "BX", badge: "SIGNATURE", sizes: [{ name: "Size S", priceVnd: 32000 }, { name: "Size M", priceVnd: 39000 }], description: "Sữa nhiều cà phê ít, ngọt béo nhẹ nhàng cho người thích vị dịu." },
  { slug: "ca-phe-trung", name: "Cà phê trứng", category: "Cà phê Việt", code: "CFT", priceVnd: 35000, badge: "SIGNATURE", description: "Cà phê trứng Hà Nội phủ lớp kem trứng vàng óng, béo ngậy." },
  { slug: "ca-phe-muoi", name: "Cà phê muối", category: "Cà phê Việt", code: "CFM", priceVnd: 32000, badge: "HOT", description: "Cà phê muối Huế với lớp kem muối béo mặn lạ miệng." },
  { slug: "ca-phe-dua", name: "Cà phê dừa", category: "Cà phê Việt", code: "CFDU", priceVnd: 39000, badge: "HOT", description: "Cà phê dừa đá xay mát lạnh, béo ngọt thơm dừa tươi." },
  { slug: "ca-phe-kem-trung-muoi", name: "Cà phê kem trứng muối", category: "Cà phê Việt", code: "CFKT", priceVnd: 42000, description: "Cà phê phủ kem trứng muối béo ngậy đang làm mưa làm gió." },
  { slug: "ca-phe-yogurt", name: "Cà phê yogurt", category: "Cà phê Việt", code: "CFYT", priceVnd: 39000, description: "Yogurt chua thanh kết hợp cà phê đậm, lạ mà cuốn." },
  { slug: "cold-brew-ca-phe", name: "Cold brew cà phê", category: "Cà phê Việt", code: "CB", priceVnd: 35000, description: "Cold brew ủ lạnh 16 giờ, vị tròn mượt ít chua." },

  // === Cà phê Âu ===
  { slug: "espresso", name: "Espresso", category: "Cà phê Âu", code: "ESP", priceVnd: 25000, description: "Shot espresso đậm đặc từ hạt rang mộc." },
  { slug: "americano-da", name: "Americano đá", category: "Cà phê Âu", code: "AMR", priceVnd: 28000, description: "Espresso pha loãng cùng nước lọc và đá, nhẹ nhàng." },
  { slug: "ca-phe-latte", name: "Cà phê latte", category: "Cà phê Âu", code: "LAT", priceVnd: 35000, description: "Latte sữa mịn với họa tiết latte art tinh tế." },
  { slug: "cappuccino", name: "Cappuccino", category: "Cà phê Âu", code: "CAP", priceVnd: 34000, description: "Cappuccino foam dày, phủ chút bột cacao." },
  { slug: "mocha", name: "Mocha", category: "Cà phê Âu", code: "MOC", priceVnd: 38000, description: "Cà phê mocha socola ngọt đắng quyện kem tươi." },
  { slug: "caramel-macchiato", name: "Caramel macchiato", category: "Cà phê Âu", code: "CARM", priceVnd: 39000, description: "Macchiato sữa vanilla điểm tô vệt sốt caramel." },

  // === Matcha & Chocolate ===
  { slug: "matcha-latte-da", name: "Matcha latte đá", category: "Matcha & Chocolate", code: "MKL", priceVnd: 39000, badge: "NEW", description: "Matcha Nhật xay mịn hòa cùng sữa mát lạnh." },
  { slug: "chocolate-nong", name: "Chocolate nóng", category: "Matcha & Chocolate", code: "CHN", priceVnd: 35000, description: "Socola nóng đặc ruột phủ kem tươi béo ngậy." },
  { slug: "chocolate-da", name: "Chocolate đá", category: "Matcha & Chocolate", code: "CHD", priceVnd: 38000, description: "Socola đá ngọt mát cùng đá viên mịn." },

  // === Trà & Trà trái cây ===
  { slug: "tra-dao-cam-sa", name: "Trà đào cam sả", category: "Trà & Trà trái cây", code: "TDCS", badge: "HOT", sizes: [{ name: "Size M", priceVnd: 45000 }, { name: "Size L", priceVnd: 52000 }], description: "Trà đào cam sả giải nhiệt, chua ngọt hài hòa." },
  { slug: "tra-vai", name: "Trà vải", category: "Trà & Trà trái cây", code: "TV", priceVnd: 38000, badge: "HOT", description: "Trà vải thơm ngọt với miếng vải tươi mọng nước." },
  { slug: "tra-chanh-da", name: "Trà chanh đá", category: "Trà & Trà trái cây", code: "TCH", priceVnd: 18000, description: "Trà chanh đá dân dã, chua thanh giải khát." },
  { slug: "tra-tac", name: "Trà tắc", category: "Trà & Trà trái cây", code: "TT", priceVnd: 18000, description: "Trà tắc chua nồng hậu, giá mềm thơm vỏ." },
  { slug: "tra-o-long-cao-son", name: "Trà ô long cao sơn", category: "Trà & Trà trái cây", code: "TOL", priceVnd: 25000, description: "Trà ô long cao sơn thanh tao, hậu ngọt sâu." },
  { slug: "tra-atiso", name: "Trà atiso", category: "Trà & Trà trái cây", code: "TAT", priceVnd: 25000, description: "Trà atiso Đà Lạt sắc đỏ dịu, mát gan giải nhiệt." },
  { slug: "tra-gung", name: "Trà gừng", category: "Trà & Trà trái cây", code: "TG", priceVnd: 22000, description: "Trà gừng nóng ấm bụng, pha cùng mật ong." },
  { slug: "tra-xoai", name: "Trà xoài", category: "Trà & Trà trái cây", code: "TX", priceVnd: 38000, description: "Trà xoài nhiệt đới vàng ươm, ngọt thơm cơn gió." },
  { slug: "tra-dau", name: "Trà dâu", category: "Trà & Trà trái cây", code: "TD", priceVnd: 38000, description: "Trà dâu hồng ngọt với dâu tươi cắt lát." },
  { slug: "tra-mang-cau", name: "Trà mãng cầu", category: "Trà & Trà trái cây", code: "TMC", priceVnd: 39000, badge: "NEW", description: "Trà mãng cầu béo mịn, vị chua ngọt nhiệt đới." },

  // === Trà sữa ===
  { slug: "tra-sua-tran-chau", name: "Trà sữa trân châu", category: "Trà sữa", code: "TSTC", priceVnd: 35000, description: "Trà sữa truyền thống với trân châu đen dai ngon." },
  { slug: "tra-sua-tran-chau-duong-den", name: "Trà sữa trân châu đường đen", category: "Trà sữa", code: "TSDD", priceVnd: 42000, badge: "BEST_SELLER", description: "Trà sữa đường đen vệt loang, trân châu mật đường." },
  { slug: "tra-sua-matcha", name: "Trà sữa matcha", category: "Trà sữa", code: "TSM", priceVnd: 39000, description: "Trà sữa matcha xanh mát cho tín đồ trà xanh." },
  { slug: "tra-sua-khoai-mon", name: "Trà sữa khoai môn", category: "Trà sữa", code: "TSKM", priceVnd: 39000, description: "Trà sữa khoai môn tím pastel béo ngậy." },
  { slug: "tra-sua-suong-sao", name: "Trà sữa sương sáo", category: "Trà sữa", code: "TSSS", priceVnd: 38000, description: "Trà sữa sương sáo mát lạnh, thanh nhiệt ngày nắng." },

  // === Đá xay ===
  { slug: "da-xay-chocolate", name: "Đá xay chocolate", category: "Đá xay", code: "DXCH", priceVnd: 42000, badge: "HOT", description: "Đá xay socola êm mịn phủ kem tươi và sốt socola." },
  { slug: "da-xay-matcha", name: "Đá xay matcha", category: "Đá xay", code: "DXM", priceVnd: 42000, description: "Đá xay matcha tuyết xanh, đắng nhẹ hậu ngọt." },
  { slug: "da-xay-caramel", name: "Đá xay caramel", category: "Đá xay", code: "DXC", priceVnd: 42000, description: "Đá xay caramel rưới sốt vàng óng ánh." },
  { slug: "da-xay-oreo", name: "Đá xay Oreo", category: "Đá xay", code: "DXO", priceVnd: 45000, description: "Đá xay Oreo béo ngậy với vụn bánh quy đen." },
  { slug: "da-xay-dau", name: "Đá xay dâu", category: "Đá xay", code: "DXD", priceVnd: 40000, description: "Đá xay dâu hồng ngọt, chua nhẹ dễ chịu." },

  // === Sinh tố ===
  { slug: "sinh-to-bo", name: "Sinh tố bơ", category: "Sinh tố", code: "STB", priceVnd: 39000, badge: "BEST_SELLER", description: "Sinh tố bơ Việt đặc sánh, phủ lớp sữa đặc." },
  { slug: "sinh-to-xoai", name: "Sinh tố xoài", category: "Sinh tố", code: "STX", priceVnd: 35000, description: "Sinh tố xoài chín vàng, ngọt thơm tự nhiên." },
  { slug: "sinh-to-dau", name: "Sinh tố dâu", category: "Sinh tố", code: "STD", priceVnd: 35000, description: "Sinh tố dâu tươi hồng ngọt ngào." },
  { slug: "sinh-to-chuoi", name: "Sinh tố chuối", category: "Sinh tố", code: "STC", priceVnd: 32000, description: "Sinh tố chuối kem mịn bổ dưỡng." },
  { slug: "sinh-to-mang-cau", name: "Sinh tố mãng cầu", category: "Sinh tố", code: "STMC", priceVnd: 39000, description: "Sinh tố mãng cầu mềm mịn, chua ngọt nhiệt đới." },
  { slug: "sinh-to-sa-po-che", name: "Sinh tố sa pô chê", category: "Sinh tố", code: "STSP", priceVnd: 35000, description: "Sinh tố sa pô chê ngọt bùi hương mật ong." },
  { slug: "sinh-to-chanh-day", name: "Sinh tố chanh dây", category: "Sinh tố", code: "STCD", priceVnd: 32000, badge: "NEW", description: "Sinh tố chanh dây vàng tươi chua ngọt tưa lưỡi." },

  // === Nước ép ===
  { slug: "nuoc-ep-cam", name: "Nước ép cam", category: "Nước ép", code: "NEC", priceVnd: 25000, description: "Nước ép cam vắt tươi 100%, nguyên vị." },
  { slug: "nuoc-ep-dua-hau", name: "Nước ép dưa hấu", category: "Nước ép", code: "NEDH", priceVnd: 25000, description: "Nước ép dưa hấu đỏ mọng giải nhiệt mùa hè." },
  { slug: "nuoc-ep-dua", name: "Nước ép dứa", category: "Nước ép", code: "NED", priceVnd: 25000, description: "Nước ép dứa vàng óng chua ngọt tươi mát." },
  { slug: "nuoc-ep-tao", name: "Nước ép táo", category: "Nước ép", code: "NET", priceVnd: 28000, description: "Nước ép táo xanh thanh mát nhẹ nhàng." },
  { slug: "nuoc-ep-dua-luoi", name: "Nước ép dưa lưới", category: "Nước ép", code: "NEDL", priceVnd: 28000, description: "Nước ép dưa lưới ngọt lịm thơm mát." },
  { slug: "nuoc-ep-oi", name: "Nước ép ổi", category: "Nước ép", code: "NEO", priceVnd: 28000, description: "Nước ép ổi hồng sánh mịn, giàu vitamin C." },
  { slug: "nuoc-ep-nho", name: "Nước ép nho", category: "Nước ép", code: "NEN", priceVnd: 30000, description: "Nước ép nho tím đậm sắc, vị ngọt thanh." },

  // === Soda & Mojito ===
  { slug: "soda-chanh", name: "Soda chanh", category: "Soda & Mojito", code: "SDC", priceVnd: 18000, description: "Soda chanh bạc hà sủi bọt mát lạnh." },
  { slug: "soda-dua", name: "Soda dứa", category: "Soda & Mojito", code: "SDD", priceVnd: 22000, description: "Soda dứa vàng ươm, sủi tăm tươi mát." },
  { slug: "soda-viet-quat", name: "Soda việt quất", category: "Soda & Mojito", code: "SDVQ", priceVnd: 25000, badge: "NEW", description: "Soda việt quất tím biếc với việt quất tươi." },
  { slug: "mojito-chanh-bac-ha", name: "Mojito chanh bạc hà", category: "Soda & Mojito", code: "MJC", priceVnd: 25000, description: "Mojito chanh bạc hà không cồn, mát lạnh tràn bọt." },
  { slug: "mojito-dau", name: "Mojito dâu", category: "Soda & Mojito", code: "MJD", priceVnd: 28000, description: "Mojito dâu hồng, chua ngọt sủi bọt trong veo." },

  // === Sữa chua & Kem ===
  { slug: "sua-chua-nha-dam", name: "Sữa chua nha đam", category: "Sữa chua & Kem", code: "SCND", priceVnd: 28000, description: "Sữa chua nha đam giòn sần, thanh mát dễ chịu." },
  { slug: "sua-chua-chanh-day", name: "Sữa chua chanh dây", category: "Sữa chua & Kem", code: "SCCD", priceVnd: 28000, description: "Sữa chua chanh dây chua ngọt cân bằng." },
  { slug: "sua-chua-dau", name: "Sữa chua dâu", category: "Sữa chua & Kem", code: "SCD", priceVnd: 30000, description: "Sữa chua dâu hồng ngọt cùng dâu tươi." },
  { slug: "kem-dua", name: "Kem dừa", category: "Sữa chua & Kem", code: "KD", priceVnd: 35000, badge: "SIGNATURE", description: "Kem dừa phục vụ trong vỏ dừa tươi, rắc đậu phộng rang." },
  { slug: "affogato", name: "Affogato", category: "Sữa chua & Kem", code: "AFF", priceVnd: 45000, badge: "SIGNATURE", description: "Kem vanilla chan espresso nóng đậm đà." },

  // === Đồ ăn nhẹ ===
  { slug: "croissant-bo-toi", name: "Croissant bơ tỏi", category: "Đồ ăn nhẹ", code: "CBT", priceVnd: 35000, badge: "HOT", description: "Croissant bơ tỏi vàng giòn, thơm mùi rau thơm." },
  { slug: "croissant-bo", name: "Croissant bơ", category: "Đồ ăn nhẹ", code: "CRO", priceVnd: 30000, description: "Croissant bơ Pháp lớp vỏ giòn, ruột mềm xốp." },
  { slug: "banh-mi-que-pa-te", name: "Bánh mì que pa tê", category: "Đồ ăn nhẹ", code: "BMQ", priceVnd: 15000, description: "Bánh mì que pa tê giòn tan, tinh hoa đường phố Sài Gòn." },
  { slug: "banh-mi-thit-nuong", name: "Bánh mì thịt nướng", category: "Đồ ăn nhẹ", code: "BMTN", priceVnd: 35000, badge: "BEST_SELLER", description: "Bánh mì thịt nướng than hoa đầy ắp đồ chua rau thơm." },
  { slug: "banh-flan-caramel", name: "Bánh flan caramel", category: "Đồ ăn nhẹ", code: "BFC", priceVnd: 15000, description: "Bánh flan caramel mềm mịn, vị béo ngọt thanh." },
  { slug: "banh-tart-trung", name: "Bánh tart trứng", category: "Đồ ăn nhẹ", code: "BTT", priceVnd: 18000, description: "Bánh tart trứng vỏ ngàn lớp, nhân kem béo." },
  { slug: "banh-donut", name: "Bánh donut", category: "Đồ ăn nhẹ", code: "BDN", priceVnd: 20000, description: "Donut phủ glaze dâu với vụn màu rực rỡ." },
  { slug: "banh-brownie", name: "Bánh brownie", category: "Đồ ăn nhẹ", code: "BBB", priceVnd: 22000, description: "Brownie socola đặc ruột, đậm vị cacao." },
  { slug: "banh-tiramisu", name: "Bánh tiramisu", category: "Đồ ăn nhẹ", code: "BTM", priceVnd: 35000, badge: "CHEF_PICK", description: "Tiramisu mascarpone thấm cà phê, phủ bột cacao." },
  { slug: "banh-cheesecake", name: "Bánh cheesecake", category: "Đồ ăn nhẹ", code: "BC", priceVnd: 35000, badge: "CHEF_PICK", description: "Cheesecake New York béo mịn, sốt trái cây rừng." },
  { slug: "khoai-tay-chien", name: "Khoai tây chiên", category: "Đồ ăn nhẹ", code: "KTC", priceVnd: 25000, description: "Khoai tây chiên giòn rụm ăn kèm sốt." },
  { slug: "ga-chien-gion", name: "Gà chiên giòn", category: "Đồ ăn nhẹ", code: "GCG", priceVnd: 45000, description: "Gà chiên muối ớt giòn rụm, thấm đều gia vị." },
];
